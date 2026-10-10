# midi

[![Go Reference](https://pkg.go.dev/badge/github.com/cwbudde/midi.svg)](https://pkg.go.dev/github.com/cwbudde/midi)
[![CI](https://github.com/cwbudde/midi/actions/workflows/ci.yml/badge.svg)](https://github.com/cwbudde/midi/actions/workflows/ci.yml)

A small, dependency-free Go package for writing and reading
[Standard MIDI Files](https://midi.org/standard-midi-files) (SMF).

The `smf` package writes format-1 files with deterministic, byte-stable
output, which makes it a good fit for generated MIDI (transcriptions, analysis
results, test fixtures) that is checked into a repository or compared in tests:

- explicit note-off events (`8n kk 00`), never note-on with velocity 0,
- no running status: every channel event carries its status byte,
- simultaneous events are ordered meta, note-off, other channel events,
  note-on, with insertion order breaking remaining ties, so a note can end and
  restart on the same tick,
- a fixed resolution of 480 ticks per quarter note (`smf.PPQ`).

The reader parses such files (channel, meta and SysEx events without running
status) back into absolute-tick events.

## Installation

```sh
go get github.com/cwbudde/midi
```

Requires Go 1.25 or later. The module has no dependencies outside the standard
library.

## Usage

```go
package main

import (
	"log"
	"os"

	"github.com/cwbudde/midi/smf"
)

func main() {
	conductor := &smf.Track{}
	conductor.Name("conductor")
	conductor.Tempo(0, 120)
	conductor.TimeSignature(0, 4, 4)
	conductor.KeySignature(0, 0, false) // C major
	conductor.Marker(4*smf.PPQ, "verse")

	piano := &smf.Track{}
	piano.Name("piano")
	piano.Program(0, 0, 0) // channel 1, acoustic grand piano
	for i, key := range []int{60, 64, 67, 72} {
		on := i * smf.PPQ
		piano.Note(0, key, 100, on, on+smf.PPQ) // quarter notes C E G C
	}

	f, err := os.Create("song.mid")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	if err := smf.Write(f, []*smf.Track{conductor, piano}); err != nil {
		log.Fatal(err)
	}
}
```

Reading a file back:

```go
f, err := smf.Read(r)
if err != nil {
	log.Fatal(err)
}

for i, track := range f.Tracks {
	for _, e := range track { // absolute ticks, end-of-track included
		fmt.Printf("track %d tick %d % X\n", i, e.Tick, e.Data)
	}
}
```

Ticks are absolute. Events can be added to a `Track` in any order; `Write`
sorts a copy of them and converts to delta times. Arbitrary messages can be
appended to `Track.Events` directly as long as `Data` holds the complete
message (status byte included).

## API overview

| Identifier                                                    | Purpose                                               |
| ------------------------------------------------------------- | ----------------------------------------------------- |
| `PPQ`                                                         | Ticks per quarter note written into the header (480)  |
| `Event`                                                       | One message at an absolute tick                       |
| `Track`                                                       | Event list of one track, with helper methods          |
| `Track.Name`, `Text`, `Marker`                                | Name (FF 03), text (FF 01) and marker (FF 06) events  |
| `Track.Tempo`, `TimeSignature`, `KeySignature`                | Tempo (FF 51), meter (FF 58) and key (FF 59) events   |
| `Track.Program`, `Note`                                       | Program change and note-on/note-off pairs             |
| `Track.ControlChange`, `PitchBend`                            | Control change (Bn) and signed pitch bend (En) events |
| `VLQ`                                                         | MIDI variable-length quantity encoding                |
| `Write`                                                       | Emit a format-1 file                                  |
| `Read`, `File`                                                | Parse a file into tracks of absolute-tick events      |

## Why a separate module?

The code was extracted from an audio analysis application. The DSP library
[`algo-dsp`](https://github.com/cwbudde/algo-dsp) deliberately excludes file
container formats (WAV, AIFF, FLAC, ...) to stay algorithm-centric, so MIDI
file I/O lives in its own module, just like WAV support lives in
[`github.com/cwbudde/wav`](https://github.com/cwbudde/wav). Applications that
only need to emit MIDI do not pull in any DSP code, and vice versa.

## Development

```sh
just test      # go test ./...
just test-race # go test -race ./...
just lint      # golangci-lint
just fmt       # gofmt
just fuzz      # fuzz the reader for 30s
```

## License

MIT, see [LICENSE](LICENSE).
