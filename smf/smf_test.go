package smf

import (
	"bytes"
	"errors"
	"io"
	"math"
	"slices"
	"testing"
)

func TestVLQ(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n    uint32
		want []byte
	}{
		{0, []byte{0x00}},
		{0x40, []byte{0x40}},
		{0x7F, []byte{0x7F}},
		{0x80, []byte{0x81, 0x00}},
		{0x2000, []byte{0xC0, 0x00}},
		{0x3FFF, []byte{0xFF, 0x7F}},
		{0x4000, []byte{0x81, 0x80, 0x00}},
		{0x1FFFFF, []byte{0xFF, 0xFF, 0x7F}},
		{0x200000, []byte{0x81, 0x80, 0x80, 0x00}},
		{0x08000000, []byte{0xC0, 0x80, 0x80, 0x00}},
		{0x0FFFFFFF, []byte{0xFF, 0xFF, 0xFF, 0x7F}},
	}
	for _, tt := range tests {
		if got := VLQ(tt.n); !bytes.Equal(got, tt.want) {
			t.Errorf("VLQ(%#x) = % X, want % X", tt.n, got, tt.want)
		}
	}
}

// TestVLQRoundTrip decodes VLQ output with the reader's track parser.
func TestVLQRoundTrip(t *testing.T) {
	t.Parallel()

	for _, n := range []uint32{0, 1, 0x7F, 0x80, 0x3FFF, 0x4000, 0x1FFFFF, 0x200000, 0x0FFFFFFF} {
		body := append(VLQ(n), 0xFF, 0x2F, 0x00)

		events, err := parseTrack(body)
		if err != nil {
			t.Fatalf("parse VLQ(%#x): %v", n, err)
		}

		if len(events) != 1 || events[0].Tick != int(n) {
			t.Errorf("VLQ(%#x) decoded as %+v", n, events)
		}
	}
}

func TestGolden(t *testing.T) {
	t.Parallel()

	tr := &Track{}
	tr.Note(0, 60, 100, 0, 480)
	tr.Program(0, 0, 5)
	tr.Marker(480, "x")
	tr.Note(0, 62, 90, 480, 960)
	tr.Name("t")
	tr.Tempo(0, 120)

	want := []byte{
		'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 1, 0, 1, 0x01, 0xE0,
		'M', 'T', 'r', 'k', 0, 0, 0, 42,
		0x00, 0xFF, 0x03, 0x01, 't', // tick 0: name (meta, inserted before tempo)
		0x00, 0xFF, 0x51, 0x03, 0x07, 0xA1, 0x20, // tempo 500000 µs
		0x00, 0xC0, 0x05, // program change (other)
		0x00, 0x90, 60, 100, // note-on last
		0x83, 0x60, 0xFF, 0x06, 0x01, 'x', // tick 480: marker first
		0x00, 0x80, 60, 0x00, // then note-off
		0x00, 0x90, 62, 90, // then note-on
		0x83, 0x60, 0x80, 62, 0x00, // tick 960
		0x00, 0xFF, 0x2F, 0x00, // end of track
	}

	var b bytes.Buffer
	if err := Write(&b, []*Track{tr}); err != nil {
		t.Fatal(err)
	}

	if got := b.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("output\n% X\nwant\n% X", got, want)
	}
}

func TestWriteEmpty(t *testing.T) {
	t.Parallel()

	var b bytes.Buffer
	if err := Write(&b, []*Track{{}}); err != nil {
		t.Fatal(err)
	}

	want := []byte{
		'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 1, 0, 1, 0x01, 0xE0,
		'M', 'T', 'r', 'k', 0, 0, 0, 4, 0x00, 0xFF, 0x2F, 0x00,
	}
	if !bytes.Equal(b.Bytes(), want) {
		t.Fatalf("output % X, want % X", b.Bytes(), want)
	}
}

func TestWriteDoesNotModifyTrack(t *testing.T) {
	t.Parallel()

	tr := &Track{}
	tr.Note(0, 60, 100, 480, 960)
	tr.Name("late name")

	before := slices.Clone(tr.Events)
	if err := Write(io.Discard, []*Track{tr}); err != nil {
		t.Fatal(err)
	}

	if !slices.EqualFunc(tr.Events, before, func(a, b Event) bool {
		return a.Tick == b.Tick && bytes.Equal(a.Data, b.Data)
	}) {
		t.Fatalf("events modified: %+v", tr.Events)
	}
}

func TestWriteErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tracks []*Track
	}{
		{"nil track", []*Track{nil}},
		{"empty data", []*Track{{Events: []Event{{Tick: 0}}}}},
		{"negative tick", []*Track{{Events: []Event{{Tick: -1, Data: []byte{0xC0, 0}}}}}},
		{"delta too large", []*Track{{Events: []Event{{Tick: maxVLQ + 1, Data: []byte{0xC0, 0}}}}}},
		{"too many tracks", make([]*Track, math.MaxUint16+1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var b bytes.Buffer
			if err := Write(&b, tt.tracks); err == nil {
				t.Fatal("Write succeeded, want error")
			}

			if b.Len() != 0 {
				t.Fatalf("Write emitted %d bytes despite the error", b.Len())
			}
		})
	}
}

func TestWriteMaxDelta(t *testing.T) {
	t.Parallel()

	tr := &Track{Events: []Event{{Tick: maxVLQ, Data: []byte{0xC0, 0}}}}
	if err := Write(io.Discard, []*Track{tr}); err != nil {
		t.Fatalf("delta %#x rejected: %v", maxVLQ, err)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestWritePropagatesWriterError(t *testing.T) {
	t.Parallel()

	if err := Write(failWriter{}, []*Track{{}}); err == nil {
		t.Fatal("Write succeeded, want writer error")
	}
}

func TestTrackHelpers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		add  func(*Track)
		want []Event
	}{
		{"name", func(t *Track) { t.Name("ab") }, []Event{{0, []byte{0xFF, 0x03, 2, 'a', 'b'}}}},
		{"text", func(t *Track) { t.Text(5, "C") }, []Event{{5, []byte{0xFF, 0x01, 1, 'C'}}}},
		{"marker", func(t *Track) { t.Marker(7, "") }, []Event{{7, []byte{0xFF, 0x06, 0}}}},
		{"program masks", func(t *Track) { t.Program(1, 17, 200) }, []Event{{1, []byte{0xC1, 200 & 127}}}},
		{"time signature 6/8", func(t *Track) { t.TimeSignature(0, 6, 8) }, []Event{{0, []byte{0xFF, 0x58, 4, 6, 3, 24, 8}}}},
		{"tempo 105", func(t *Track) { t.Tempo(0, 105) }, []Event{{0, []byte{0xFF, 0x51, 3, 0x08, 0xB8, 0x25}}}},
		{"key Bb major", func(t *Track) { t.KeySignature(0, -2, false) }, []Event{{0, []byte{0xFF, 0x59, 2, 0xFE, 0}}}},
		{"key E minor", func(t *Track) { t.KeySignature(0, 1, true) }, []Event{{0, []byte{0xFF, 0x59, 2, 1, 1}}}},
		{"negative tick clamps", func(t *Track) { t.Text(-10, "") }, []Event{{0, []byte{0xFF, 0x01, 0}}}},
		{
			"note", func(t *Track) { t.Note(9, 36, 127, 0, 60) },
			[]Event{{0, []byte{0x99, 36, 127}}, {60, []byte{0x89, 36, 0}}},
		},
		{
			"note velocity clamps", func(t *Track) { t.Note(0, 60, 0, 0, 10); t.Note(0, 61, 300, 0, 10) },
			[]Event{{0, []byte{0x90, 60, 1}}, {10, []byte{0x80, 60, 0}}, {0, []byte{0x90, 61, 127}}, {10, []byte{0x80, 61, 0}}},
		},
		{
			"note off at least on+1", func(t *Track) { t.Note(0, 60, 64, 100, 50) },
			[]Event{{100, []byte{0x90, 60, 64}}, {101, []byte{0x80, 60, 0}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := &Track{}
			tt.add(tr)

			if !slices.EqualFunc(tr.Events, tt.want, func(a, b Event) bool {
				return a.Tick == b.Tick && bytes.Equal(a.Data, b.Data)
			}) {
				t.Fatalf("events %v, want %v", tr.Events, tt.want)
			}
		})
	}
}

func TestLongMetaLength(t *testing.T) {
	t.Parallel()

	tr := &Track{}
	tr.Text(0, string(bytes.Repeat([]byte{'a'}, 200)))

	if got := tr.Events[0].Data[:4]; !bytes.Equal(got, []byte{0xFF, 0x01, 0x81, 0x48}) {
		t.Fatalf("meta prefix % X", got)
	}
}

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	conductor := &Track{}
	conductor.Name("conductor")
	conductor.Tempo(0, 105)
	conductor.TimeSignature(0, 4, 4)
	conductor.Marker(960, "drop")

	lead := &Track{}
	lead.Program(0, 0, 80)
	lead.Note(0, 64, 100, 480, 960)
	lead.Note(0, 67, 90, 960, 1200) // note-on at the tick of the previous note-off

	drums := &Track{}
	drums.Note(9, 36, 127, 0, 60)

	var b bytes.Buffer
	if err := Write(&b, []*Track{conductor, lead, drums}); err != nil {
		t.Fatal(err)
	}

	raw := b.Bytes()
	if !bytes.Equal(raw[:14], []byte{'M', 'T', 'h', 'd', 0, 0, 0, 6, 0, 1, 0, 3, 0x01, 0xE0}) {
		t.Fatalf("header % X", raw[:14])
	}

	f, err := Read(&b)
	if err != nil {
		t.Fatal(err)
	}

	if f.Format != 1 || f.Division != 480 || len(f.Tracks) != 3 {
		t.Fatalf("file %+v", f)
	}

	find := func(track []Event, prefix ...byte) *Event {
		for i := range track {
			if bytes.HasPrefix(track[i].Data, prefix) {
				return &track[i]
			}
		}

		return nil
	}

	if e := find(f.Tracks[0], 0xFF, 0x51, 3); e == nil || !bytes.Equal(e.Data[3:], []byte{0x08, 0xB8, 0x25}) { // 571429 µs
		t.Fatalf("tempo %+v", e)
	}

	if e := find(f.Tracks[0], 0xFF, 0x58, 4); e == nil || !bytes.Equal(e.Data[3:], []byte{4, 2, 0x18, 8}) {
		t.Fatalf("time signature %+v", e)
	}

	if e := find(f.Tracks[0], 0xFF, 0x06); e == nil || e.Tick != 960 || string(e.Data[3:]) != "drop" {
		t.Fatalf("marker %+v", e)
	}

	for _, track := range f.Tracks {
		if last := track[len(track)-1]; !bytes.Equal(last.Data, []byte{0xFF, 0x2F, 0}) {
			t.Fatalf("missing end of track: %+v", last)
		}
	}

	notes := []Event{}

	for _, e := range f.Tracks[1] {
		if e.Data[0]&0xE0 == 0x80 {
			notes = append(notes, e)
		}
	}

	want := []Event{{480, []byte{0x90, 64, 100}}, {960, []byte{0x80, 64, 0}}, {960, []byte{0x90, 67, 90}}, {1200, []byte{0x80, 67, 0}}}
	if len(notes) != len(want) {
		t.Fatalf("notes %+v", notes)
	}

	for i := range want {
		if notes[i].Tick != want[i].Tick || !bytes.Equal(notes[i].Data, want[i].Data) {
			t.Fatalf("note event %d: %+v, want %+v", i, notes[i], want[i])
		}
	}

	if e := find(f.Tracks[2], 0x99, 36); e == nil || e.Tick != 0 {
		t.Fatalf("drum note %+v", e)
	}
}

// TestRewriteIsIdentity checks that reading a written file and writing its
// events again (minus end-of-track) reproduces the same bytes.
func TestRewriteIsIdentity(t *testing.T) {
	t.Parallel()

	a, b := &Track{}, &Track{}
	a.Name("a")
	a.Tempo(0, 93.5)
	a.KeySignature(0, -3, true)
	a.Text(17, "C#m7")
	b.Program(0, 3, 38)

	for i := range 50 {
		b.Note(3, 40+i%12, 30+i, i*113, i*113+(i%5)*97)
	}

	var first bytes.Buffer
	if err := Write(&first, []*Track{a, b}); err != nil {
		t.Fatal(err)
	}

	f, err := Read(bytes.NewReader(first.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	tracks := make([]*Track, len(f.Tracks))
	for i, events := range f.Tracks {
		tracks[i] = &Track{Events: events[:len(events)-1]}
	}

	var second bytes.Buffer
	if err := Write(&second, tracks); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("rewritten file differs")
	}
}

func TestReadSysExAndOtherChannelMessages(t *testing.T) {
	t.Parallel()

	body := []byte{
		0x00, 0xF0, 0x03, 0x7E, 0x09, 0xF7, // SysEx
		0x00, 0xB0, 0x07, 0x64, // control change
		0x00, 0xD0, 0x40, // channel pressure
		0x00, 0xE0, 0x00, 0x40, // pitch bend
		0x00, 0xF7, 0x01, 0xF8, // SysEx escape
		0x00, 0xFF, 0x2F, 0x00,
	}

	events, err := parseTrack(body)
	if err != nil {
		t.Fatal(err)
	}

	if len(events) != 6 {
		t.Fatalf("%d events, want 6: %v", len(events), events)
	}

	if !bytes.Equal(events[0].Data, []byte{0xF0, 0x03, 0x7E, 0x09, 0xF7}) {
		t.Fatalf("SysEx % X", events[0].Data)
	}
}

func TestReadErrors(t *testing.T) {
	t.Parallel()

	tr := &Track{}
	tr.Name("abc")
	tr.Note(0, 60, 100, 0, 480)

	var b bytes.Buffer
	if err := Write(&b, []*Track{tr}); err != nil {
		t.Fatal(err)
	}

	valid := b.Bytes()
	header := valid[:14]
	track := func(body ...byte) []byte {
		out := append(slices.Clone(header), 'M', 'T', 'r', 'k', 0, 0, 0, byte(len(body)))
		return append(out, body...)
	}

	tests := []struct {
		name   string
		input  []byte
		target error // nil: any error
	}{
		{"empty", nil, io.EOF},
		{"short header", valid[:5], io.ErrUnexpectedEOF},
		{"header payload cut", valid[:10], io.ErrUnexpectedEOF},
		{"no tracks", header, io.ErrUnexpectedEOF},
		{"track header cut", valid[:18], io.ErrUnexpectedEOF},
		{"track body cut", valid[:len(valid)-1], io.ErrUnexpectedEOF},
		{"forged huge chunk", append(slices.Clone(header), 'M', 'T', 'r', 'k', 0xFF, 0xFF, 0xFF, 0xFF, 0), io.ErrUnexpectedEOF},
		{"wrong magic", append([]byte("RIFF"), valid[4:]...), nil},
		{"long header", append([]byte{'M', 'T', 'h', 'd', 0, 0, 0, 7}, make([]byte, 7)...), nil},
		{"unexpected chunk", append(slices.Clone(header), 'X', 'T', 'r', 'k', 0, 0, 0, 0), nil},
		{"delta cut", track(0x81), io.ErrUnexpectedEOF},
		{"VLQ too long", track(0x81, 0x81, 0x81, 0x81, 0x00), nil},
		{"status missing", track(0x00), io.ErrUnexpectedEOF},
		{"note cut", track(0x00, 0x90, 60), io.ErrUnexpectedEOF},
		{"program cut", track(0x00, 0xC0), io.ErrUnexpectedEOF},
		{"meta type cut", track(0x00, 0xFF), io.ErrUnexpectedEOF},
		{"meta payload cut", track(0x00, 0xFF, 0x01, 0x05, 'a'), io.ErrUnexpectedEOF},
		{"SysEx payload cut", track(0x00, 0xF0, 0x05, 0x7E), io.ErrUnexpectedEOF},
		{"running status", track(0x00, 0x90, 60, 100, 0x00, 62, 100), nil},
		{"system realtime", track(0x00, 0xF8), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f, err := Read(bytes.NewReader(tt.input))
			if err == nil {
				t.Fatalf("Read succeeded: %+v", f)
			}

			if tt.target != nil && !errors.Is(err, tt.target) {
				t.Fatalf("error %v, want %v", err, tt.target)
			}
		})
	}
}

func FuzzRead(f *testing.F) {
	tr := &Track{}
	tr.Name("fuzz")
	tr.Tempo(0, 120)
	tr.TimeSignature(0, 3, 4)
	tr.Note(0, 60, 100, 0, 480)
	tr.Program(240, 1, 10)

	var b bytes.Buffer
	if err := Write(&b, []*Track{tr, {}}); err != nil {
		f.Fatal(err)
	}

	f.Add(b.Bytes())
	f.Add([]byte{})
	f.Add([]byte("MThd\x00\x00\x00\x06\x00\x00\x00\x01\x00\x60MTrk\x00\x00\x00\x04\x00\xFF\x2F\x00"))

	f.Fuzz(func(t *testing.T, data []byte) {
		file, err := Read(bytes.NewReader(data))
		if err != nil {
			return
		}

		for _, events := range file.Tracks {
			tick := 0

			for _, e := range events {
				if len(e.Data) == 0 {
					t.Fatal("event with empty data")
				}

				if e.Tick < tick {
					t.Fatalf("tick went backwards: %d after %d", e.Tick, tick)
				}

				tick = e.Tick
			}
		}
	})
}

func TestNoteBeforeStartStillEnds(t *testing.T) {
	t.Parallel()

	var tr Track
	tr.Note(0, 60, 100, -30, -5)

	if len(tr.Events) != 2 {
		t.Fatalf("got %d events, want 2", len(tr.Events))
	}

	on, off := tr.Events[0], tr.Events[1]
	if on.Tick != 0 || off.Tick != 1 {
		t.Fatalf("note-on at %d and note-off at %d, want 0 and 1", on.Tick, off.Tick)
	}
}
