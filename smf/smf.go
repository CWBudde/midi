package smf

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
)

// PPQ is the tick resolution per quarter note written into every file
// header by [Write].
const PPQ = 480

// maxVLQ is the largest value a MIDI variable-length quantity can hold
// (four bytes of seven bits each).
const maxVLQ = 1<<28 - 1

// Event is one timed message. Tick is absolute (not a delta). Data is the
// complete message: status and data bytes for channel events, or
// FF type length payload for meta events.
type Event struct {
	Tick int
	Data []byte
}

// order ranks simultaneous events: meta, note-off, other channel, note-on.
// Note-offs before note-ons let a note end and restart on the same tick
// without the new note being cut off.
func (e Event) order() int {
	switch s := e.Data[0]; {
	case s == 0xFF:
		return 0
	case s&0xF0 == 0x80:
		return 1
	case s&0xF0 == 0x90:
		return 3
	}

	return 2
}

// Track collects the events of one MIDI track. The zero value is an empty
// track ready to use. Events may be added in any order; [Write] sorts them.
// The end-of-track meta event is appended by Write and must not be added.
type Track struct{ Events []Event }

// add appends a message at tick, clamping negative ticks to 0.
func (t *Track) add(tick int, data ...byte) {
	t.Events = append(t.Events, Event{max(0, tick), data})
}

// meta appends a meta event FF kind len payload.
func (t *Track) meta(tick int, kind byte, payload []byte) {
	data := append([]byte{0xFF, kind}, VLQ(uint32(len(payload)))...)
	t.add(tick, append(data, payload...)...)
}

// Name adds a sequence/track name meta event (FF 03) at tick 0.
func (t *Track) Name(name string) { t.meta(0, 0x03, []byte(name)) }

// Text adds a text meta event (FF 01) at tick.
func (t *Track) Text(tick int, s string) { t.meta(tick, 0x01, []byte(s)) }

// Marker adds a marker meta event (FF 06) at tick.
func (t *Track) Marker(tick int, s string) { t.meta(tick, 0x06, []byte(s)) }

// Program adds a program change on channel ch (0-15) at tick. Channel and
// program are masked to their valid ranges (4 and 7 bits).
func (t *Track) Program(tick, ch, prog int) {
	t.add(tick, 0xC0|byte(ch&15), byte(prog&127))
}

// TimeSignature adds a time signature meta event (FF 58) for num/den at
// tick. den must be a power of two; it is stored as its base-2 logarithm.
// The metronome is set to 24 MIDI clocks per click and 8 thirty-second
// notes per quarter note.
func (t *Track) TimeSignature(tick, num, den int) {
	t.meta(tick, 0x58, []byte{byte(num), byte(math.Round(math.Log2(float64(den)))), 24, 8})
}

// Tempo adds a set-tempo meta event (FF 51) at tick. The tempo is stored as
// microseconds per quarter note, rounded to the nearest integer.
func (t *Track) Tempo(tick int, bpm float64) {
	us := uint32(math.Round(60e6 / bpm))
	t.meta(tick, 0x51, []byte{byte(us >> 16), byte(us >> 8), byte(us)})
}

// KeySignature adds a key signature meta event (FF 59) at tick. sharps is
// the number of sharps, negative for flats (-7..7); minor selects the minor
// mode.
func (t *Track) KeySignature(tick, sharps int, minor bool) {
	m := byte(0)
	if minor {
		m = 1
	}

	t.meta(tick, 0x59, []byte{byte(int8(sharps)), m})
}

// Note adds an explicit note-on/note-off pair for key on channel ch (0-15).
// The note-on is at tick on, the note-off (velocity 0 on status 0x8n) at
// tick off, but at least one tick after on. vel is clamped to 1..127 so the
// note-on is never mistaken for a note-off. A negative on is clamped to 0
// before the note-off is placed, so a note that starts before the file still
// ends at least one tick later instead of hanging.
func (t *Track) Note(ch, key, vel, on, off int) {
	vel = min(127, max(1, vel))
	on = max(0, on)
	t.add(on, 0x90|byte(ch&15), byte(key&127), byte(vel))
	t.add(max(on+1, off), 0x80|byte(ch&15), byte(key&127), 0)
}

// VLQ encodes n as a MIDI variable-length quantity: big-endian groups of
// seven bits, with the high bit set on every byte but the last.
//
// Valid quantities are below 2^28 and encode to one to four bytes. Larger
// values produce a five-byte sequence that is not valid in a MIDI file.
func VLQ(n uint32) []byte {
	out := []byte{byte(n & 0x7F)}
	for n >>= 7; n > 0; n >>= 7 {
		out = append([]byte{byte(n&0x7F) | 0x80}, out...)
	}

	return out
}

// Write emits a format-1 file with one MTrk chunk per track. Events are
// stably sorted by tick and kind (meta, note-off, other, note-on), so
// insertion order breaks remaining ties, and every track is terminated with
// an end-of-track meta event. The tracks are not modified.
//
// Write validates all tracks before writing anything: it returns an error
// for more than 65535 tracks, a nil track, an event with empty Data, a
// negative tick, or a delta time that does not fit in a VLQ. The whole file
// is written with a single call to w.Write.
func Write(w io.Writer, tracks []*Track) error {
	if len(tracks) > math.MaxUint16 {
		return fmt.Errorf("smf: %d tracks exceed the format limit of %d", len(tracks), math.MaxUint16)
	}

	b := []byte("MThd")
	b = binary.BigEndian.AppendUint32(b, 6)
	b = binary.BigEndian.AppendUint16(b, 1) // format 1
	b = binary.BigEndian.AppendUint16(b, uint16(len(tracks)))
	b = binary.BigEndian.AppendUint16(b, PPQ)

	for ti, t := range tracks {
		body, err := encodeTrack(t)
		if err != nil {
			return fmt.Errorf("smf: track %d: %w", ti, err)
		}

		b = append(b, "MTrk"...)
		b = binary.BigEndian.AppendUint32(b, uint32(len(body)))
		b = append(b, body...)
	}

	if _, err := w.Write(b); err != nil {
		return fmt.Errorf("smf: write: %w", err)
	}

	return nil
}

// encodeTrack returns the MTrk chunk body for t: sorted events with delta
// times, followed by end-of-track.
func encodeTrack(t *Track) ([]byte, error) {
	if t == nil {
		return nil, errors.New("nil track")
	}

	for i, e := range t.Events {
		switch {
		case len(e.Data) == 0:
			return nil, fmt.Errorf("event %d: empty data", i)
		case e.Tick < 0:
			return nil, fmt.Errorf("event %d: negative tick %d", i, e.Tick)
		}
	}

	events := slices.Clone(t.Events)
	slices.SortStableFunc(events, func(a, b Event) int {
		if c := cmp.Compare(a.Tick, b.Tick); c != 0 {
			return c
		}

		return cmp.Compare(a.order(), b.order())
	})

	var body []byte

	tick := 0

	for _, e := range events {
		delta := e.Tick - tick
		if delta > maxVLQ {
			return nil, fmt.Errorf("delta time %d at tick %d exceeds VLQ range", delta, e.Tick)
		}

		body = append(body, VLQ(uint32(delta))...)
		body = append(body, e.Data...)
		tick = e.Tick
	}

	return append(body, 0, 0xFF, 0x2F, 0), nil
}
