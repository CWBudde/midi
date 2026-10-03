package smf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// File is a parsed Standard MIDI File.
type File struct {
	// Format is the SMF format from the header (0, 1 or 2).
	Format int
	// Division is the raw time division from the header: ticks per quarter
	// note when the high bit is clear, an SMPTE frame rate and resolution
	// otherwise.
	Division int
	// Tracks holds the events of each MTrk chunk in file order with
	// absolute ticks. The end-of-track meta event is included.
	Tracks [][]Event
}

// Read parses a Standard MIDI File from r.
//
// Supported are channel messages, meta events and SysEx events (F0/F7), each
// with an explicit status byte; running status is not supported and yields
// an error. The header chunk must be exactly six bytes long and the track
// chunks must follow it directly, as in files written by [Write].
//
// Truncated input yields an error wrapping [io.ErrUnexpectedEOF], or [io.EOF]
// for empty input. Chunk payloads are never allocated beyond the data
// actually present in r, so a forged chunk length cannot exhaust memory.
func Read(r io.Reader) (*File, error) {
	id, h, err := readChunk(r)
	if err != nil {
		return nil, fmt.Errorf("smf: read header: %w", err)
	}

	if id != "MThd" || len(h) != 6 {
		return nil, errors.New("smf: missing header")
	}

	f := &File{
		Format:   int(binary.BigEndian.Uint16(h)),
		Division: int(binary.BigEndian.Uint16(h[4:])),
	}

	for ti := range int(binary.BigEndian.Uint16(h[2:])) {
		id, data, err := readChunk(r)
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF // the header promised more tracks
		}

		if err != nil {
			return nil, fmt.Errorf("smf: read track %d: %w", ti, err)
		}

		if id != "MTrk" {
			return nil, fmt.Errorf("smf: unexpected chunk %q", id)
		}

		events, err := parseTrack(data)
		if err != nil {
			return nil, fmt.Errorf("smf: track %d: %w", ti, err)
		}

		f.Tracks = append(f.Tracks, events)
	}

	return f, nil
}

// readChunk reads one chunk: a four-byte ID, a 32-bit big-endian length and
// the payload.
func readChunk(r io.Reader) (string, []byte, error) {
	var head [8]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return "", nil, err
	}

	n := int64(binary.BigEndian.Uint32(head[4:]))

	data, err := io.ReadAll(io.LimitReader(r, n))
	if err != nil {
		return "", nil, err
	}

	if int64(len(data)) < n {
		return "", nil, io.ErrUnexpectedEOF
	}

	return string(head[:4]), data, nil
}

// parseTrack splits an MTrk chunk body into events with absolute ticks.
func parseTrack(d []byte) ([]Event, error) {
	var out []Event

	tick, i := 0, 0

	vlq := func() (int, error) {
		n := 0

		for range 4 {
			if i >= len(d) {
				return 0, io.ErrUnexpectedEOF
			}

			c := d[i]
			i++

			n = n<<7 | int(c&0x7F)
			if c&0x80 == 0 {
				return n, nil
			}
		}

		return 0, errors.New("VLQ too long")
	}

	for i < len(d) {
		delta, err := vlq()
		if err != nil {
			return nil, fmt.Errorf("delta time at offset %d: %w", i, err)
		}

		tick += delta

		if i >= len(d) {
			return nil, io.ErrUnexpectedEOF
		}

		start, s := i, d[i]

		switch {
		case s == 0xFF: // meta: FF type len payload
			i += 2

			n, err := vlq()
			if err != nil {
				return nil, fmt.Errorf("meta length at offset %d: %w", start, err)
			}

			i += n
		case s == 0xF0 || s == 0xF7: // SysEx: F0/F7 len payload
			i++

			n, err := vlq()
			if err != nil {
				return nil, fmt.Errorf("SysEx length at offset %d: %w", start, err)
			}

			i += n
		case s&0xF0 == 0xC0 || s&0xF0 == 0xD0: // program change, channel pressure
			i += 2
		case s >= 0x80 && s < 0xF0: // other channel messages
			i += 3
		default:
			return nil, fmt.Errorf("unsupported status %#x at offset %d", s, start)
		}

		if i > len(d) {
			return nil, io.ErrUnexpectedEOF
		}

		out = append(out, Event{tick, append([]byte(nil), d[start:i]...)})
	}

	return out, nil
}
