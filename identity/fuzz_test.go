package identity

import "testing"

func FuzzCollectorChunking(f *testing.F) {
	f.Add([]byte(tinyFasta), uint8(3))
	f.Add([]byte(">a\r\nAC GT\r\n>b x\n\n\n>c\n"), uint8(1))
	f.Add([]byte("no header\n>\n>x\xff\nAC"), uint8(5))
	f.Fuzz(func(t *testing.T, data []byte, size uint8) {
		whole := NewCollector()
		whole.Write(data)
		wholeFact, err := whole.Fact()
		if err != nil {
			t.Fatal(err)
		}
		step := int(size)%64 + 1
		pieces := NewCollector()
		for rest := data; len(rest) > 0; {
			n := min(step, len(rest))
			pieces.Write(rest[:n])
			rest = rest[n:]
		}
		piecesFact, err := pieces.Fact()
		if err != nil {
			t.Fatal(err)
		}
		if string(wholeFact.Value) != string(piecesFact.Value) {
			t.Fatalf("whole %s, in pieces of %d %s", wholeFact.Value, step, piecesFact.Value)
		}
	})
}
