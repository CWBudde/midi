// Package smf writes and reads Standard MIDI Files (SMF).
//
// The writer produces format-1 files at a fixed resolution of [PPQ] ticks per
// quarter note. Its output is deliberately simple and deterministic:
//
//   - every note is an explicit note-on/note-off pair (no note-on with
//     velocity 0 as note-off),
//   - no running status, so every channel event carries its status byte,
//   - simultaneous events in a track are ordered meta, note-off, other
//     channel events, note-on; insertion order breaks any remaining ties,
//   - every track ends with exactly one end-of-track meta event.
//
// Build a [Track] per MIDI track with its helper methods (Name, Tempo,
// TimeSignature, Note, ...) and pass all tracks to [Write]. Event ticks are
// absolute; Write sorts each track and converts to delta times.
//
// [Read] parses files in the same style back into a [File] with absolute
// ticks. It supports channel, meta and SysEx events but not running status,
// which is enough to inspect files produced by this package and by most
// tools that also avoid running status.
//
// The package has no dependencies beyond the standard library.
package smf
