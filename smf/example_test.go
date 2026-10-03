package smf_test

import (
	"bytes"
	"fmt"
	"log"

	"github.com/cwbudde/midi/smf"
)

// Example builds a two-track file: a conductor track with tempo and meter,
// and a melody track with a program change and two notes.
func Example() {
	conductor := &smf.Track{}
	conductor.Name("conductor")
	conductor.Tempo(0, 120)
	conductor.TimeSignature(0, 4, 4)

	melody := &smf.Track{}
	melody.Name("melody")
	melody.Program(0, 0, 0)                     // channel 1, acoustic grand piano
	melody.Note(0, 60, 100, 0, smf.PPQ)         // C4, one quarter note
	melody.Note(0, 64, 100, smf.PPQ, 2*smf.PPQ) // E4, the next quarter note

	var buf bytes.Buffer
	if err := smf.Write(&buf, []*smf.Track{conductor, melody}); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%d bytes, starts with %q\n", buf.Len(), buf.Bytes()[:4])
	// Output: 97 bytes, starts with "MThd"
}

func ExampleWrite() {
	t := &smf.Track{}
	t.Note(9, 36, 127, 0, 60) // kick drum on channel 10

	var buf bytes.Buffer
	if err := smf.Write(&buf, []*smf.Track{t}); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("% X\n", buf.Bytes())
	// Output: 4D 54 68 64 00 00 00 06 00 01 00 01 01 E0 4D 54 72 6B 00 00 00 0C 00 99 24 7F 3C 89 24 00 00 FF 2F 00
}

func ExampleRead() {
	t := &smf.Track{}
	t.Marker(0, "intro")
	t.Note(0, 60, 90, 0, 240)

	var buf bytes.Buffer
	if err := smf.Write(&buf, []*smf.Track{t}); err != nil {
		log.Fatal(err)
	}

	f, err := smf.Read(&buf)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("format", f.Format, "division", f.Division)

	for _, e := range f.Tracks[0] {
		fmt.Printf("%4d % X\n", e.Tick, e.Data)
	}
	// Output:
	// format 1 division 480
	//    0 FF 06 05 69 6E 74 72 6F
	//    0 90 3C 5A
	//  240 80 3C 00
	//  240 FF 2F 00
}

func ExampleVLQ() {
	for _, n := range []uint32{0, 0x7F, 0x80, 0x3FFF, 0x4000, 0x0FFFFFFF} {
		fmt.Printf("%#08x -> % X\n", n, smf.VLQ(n))
	}
	// Output:
	// 0x00000000 -> 00
	// 0x0000007f -> 7F
	// 0x00000080 -> 81 00
	// 0x00003fff -> FF 7F
	// 0x00004000 -> 81 80 00
	// 0x0fffffff -> FF FF FF 7F
}

func ExampleTrack_Note() {
	t := &smf.Track{}
	t.Note(0, 60, 100, 0, 480)
	t.Note(0, 62, 200, 480, 480) // velocity clamps to 127, off moves to on+1

	for _, e := range t.Events {
		fmt.Printf("%3d % X\n", e.Tick, e.Data)
	}
	// Output:
	//   0 90 3C 64
	// 480 80 3C 00
	// 480 90 3E 7F
	// 481 80 3E 00
}

func ExampleTrack_Tempo() {
	t := &smf.Track{}
	t.Tempo(0, 105) // 571428.57 µs per quarter note, rounded

	fmt.Printf("% X\n", t.Events[0].Data)
	// Output: FF 51 03 08 B8 25
}
