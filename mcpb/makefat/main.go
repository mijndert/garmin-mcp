// Command makefat combines single-architecture macOS binaries into one
// universal ("fat") Mach-O binary. It needs no Apple tooling, so release
// bundles can be built on Linux.
//
//	go run ./mcpb/makefat out in1 in2 ...
package main

import (
	"debug/macho"
	"encoding/binary"
	"fmt"
	"os"
)

const (
	fatMagic   = 0xCAFEBABE
	alignBits  = 14 // 16 KiB page alignment, as lipo uses for arm64
	alignBytes = 1 << alignBits
)

func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: makefat <output> <input> <input> [...]")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "makefat:", err)
		os.Exit(1)
	}
}

type arch struct {
	cpu, sub uint32
	data     []byte
}

func run(out string, inputs []string) error {
	var arches []arch
	seen := map[uint32]bool{}
	for _, in := range inputs {
		f, err := macho.Open(in)
		if err != nil {
			return fmt.Errorf("%s: %w", in, err)
		}
		cpu, sub := uint32(f.Cpu), f.SubCpu
		f.Close()
		if seen[cpu] {
			return fmt.Errorf("%s: duplicate architecture %#x", in, cpu)
		}
		seen[cpu] = true
		data, err := os.ReadFile(in)
		if err != nil {
			return err
		}
		arches = append(arches, arch{cpu: cpu, sub: sub, data: data})
	}

	// Header: magic + count, then one 20-byte fat_arch record per input.
	headerSize := 8 + 20*len(arches)
	offset := alignUp(headerSize)
	hdr := make([]byte, 0, headerSize)
	hdr = binary.BigEndian.AppendUint32(hdr, fatMagic)
	hdr = binary.BigEndian.AppendUint32(hdr, uint32(len(arches)))
	offsets := make([]int, len(arches))
	for i, a := range arches {
		offsets[i] = offset
		hdr = binary.BigEndian.AppendUint32(hdr, a.cpu)
		hdr = binary.BigEndian.AppendUint32(hdr, a.sub)
		hdr = binary.BigEndian.AppendUint32(hdr, uint32(offset))
		hdr = binary.BigEndian.AppendUint32(hdr, uint32(len(a.data)))
		hdr = binary.BigEndian.AppendUint32(hdr, alignBits)
		offset = alignUp(offset + len(a.data))
	}

	w, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	defer w.Close()
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	pos := len(hdr)
	for i, a := range arches {
		if _, err := w.Write(make([]byte, offsets[i]-pos)); err != nil {
			return err
		}
		if _, err := w.Write(a.data); err != nil {
			return err
		}
		pos = offsets[i] + len(a.data)
	}
	return nil
}

func alignUp(n int) int { return (n + alignBytes - 1) &^ (alignBytes - 1) }
