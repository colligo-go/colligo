package identity

import (
	"bytes"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"hash"

	"github.com/colligo-go/colligo/model"
)

// ExtractorVersion must change whenever the digest or the fact layout changes.
const ExtractorVersion = "seqcol-1"

// FactKind is the kind of the fact that carries the collection digest.
const FactKind = "seqcol"

const (
	sequencePrefix    = "SQ."
	scratchSize       = 32 << 10
	maxNameBytes      = 64 << 10
	maxCollectionSize = 1 << 20
)

var (
	errNoSequences   = errors.New("the file has no sequences")
	errEmptyName     = errors.New("a record header has no name")
	errNameTooLong   = errors.New("a sequence name is longer than 64 KiB")
	errTooManyRecord = errors.New("the file has more than 1048576 sequences")
)

type parseState int

const (
	atLineStart parseState = iota
	inName
	inHeaderRest
	inSequence
)

// Collection is the Sequence Collection of a FASTA file as defined by GA4GH
// Refget Sequence Collections v1.0.0. Names are the first word of each header;
// each sequence is digested after dropping every non-letter and upper-casing.
type Collection struct {
	Names     []string
	Lengths   []int64
	Sequences []string
}

// Collector turns the bytes of a FASTA file into a Collection. It keeps the
// state of the parse between calls to Write, so the file may be cut anywhere.
// A Collector is not safe for concurrent use.
type Collector struct {
	state     parseState
	inRecord  bool
	name      []byte
	length    int64
	hasher    hash.Hash
	scratch   [scratchSize]byte
	collected Collection
	failure   error
}

func NewCollector() *Collector {
	return &Collector{hasher: sha512.New()}
}

func (c *Collector) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		switch c.state {
		case atLineStart:
			if p[0] == '>' {
				c.endRecord()
				c.beginRecord()
				c.state = inName
				p = p[1:]
			} else {
				c.state = inSequence
			}
		case inName:
			p = c.readName(p)
		case inHeaderRest:
			if i := bytes.IndexByte(p, '\n'); i >= 0 {
				c.state = atLineStart
				p = p[i+1:]
			} else {
				p = nil
			}
		case inSequence:
			i := bytes.IndexByte(p, '\n')
			segment := p
			if i >= 0 {
				segment = p[:i]
				c.state = atLineStart
				p = p[i+1:]
			} else {
				p = nil
			}
			c.addSequence(segment)
		}
	}
	return n, nil
}

func (c *Collector) readName(p []byte) []byte {
	for i, b := range p {
		switch b {
		case '\n':
			c.state = atLineStart
			return p[i+1:]
		case ' ', '\t', '\r':
			c.state = inHeaderRest
			return p[i+1:]
		}
		if len(c.name) >= maxNameBytes {
			c.fail(errNameTooLong)
			continue
		}
		c.name = append(c.name, b)
	}
	return nil
}

func (c *Collector) beginRecord() {
	c.inRecord = true
	c.name = c.name[:0]
	c.length = 0
	c.hasher.Reset()
}

func (c *Collector) addSequence(segment []byte) {
	if !c.inRecord {
		return
	}
	for len(segment) > 0 {
		out := c.scratch[:0]
		var i int
		for i = 0; i < len(segment) && len(out) < scratchSize; i++ {
			b := segment[i] | 0x20
			if b >= 'a' && b <= 'z' {
				out = append(out, b&^0x20)
			}
		}
		segment = segment[i:]
		c.hasher.Write(out)
		c.length += int64(len(out))
	}
}

func (c *Collector) endRecord() {
	if !c.inRecord {
		return
	}
	c.inRecord = false
	if len(c.name) == 0 {
		c.fail(errEmptyName)
		return
	}
	if len(c.collected.Names) >= maxCollectionSize {
		c.fail(errTooManyRecord)
		return
	}
	var sum [sha512.Size]byte
	c.hasher.Sum(sum[:0])
	c.collected.Names = append(c.collected.Names, string(c.name))
	c.collected.Lengths = append(c.collected.Lengths, c.length)
	c.collected.Sequences = append(c.collected.Sequences,
		sequencePrefix+base64.RawURLEncoding.EncodeToString(sum[:digestBytes]))
}

func (c *Collector) fail(err error) {
	if c.failure == nil {
		c.failure = err
	}
}

// Collection ends the parse and returns what was read.
func (c *Collector) Collection() (Collection, error) {
	c.endRecord()
	if c.failure != nil {
		return Collection{}, c.failure
	}
	if len(c.collected.Names) == 0 {
		return Collection{}, errNoSequences
	}
	return c.collected, nil
}

// Digests are the level 0 digest of a collection and the digest of each
// attribute, as in the Refget Sequence Collections specification.
type Digests struct {
	Collection string
	Labels     string
	Lengths    string
	Sequences  string
}

// Digest computes the level 0 digest. Only the inherent attributes, names and
// sequences, contribute to it; lengths is reported but never hashed into it.
func (c Collection) Digest() (Digests, error) {
	names, err := canonicalStrings(c.Names)
	if err != nil {
		return Digests{}, err
	}
	sequences, err := canonicalStrings(c.Sequences)
	if err != nil {
		return Digests{}, err
	}
	d := Digests{
		Labels:    Sha512t24u(names),
		Lengths:   Sha512t24u(canonicalIntegers(c.Lengths)),
		Sequences: Sha512t24u(sequences),
	}
	top := []byte(`{"names":"` + d.Labels + `","sequences":"` + d.Sequences + `"}`)
	d.Collection = Sha512t24u(top)
	return d, nil
}

// SeqcolValue is the value of the seqcol fact. When the collection cannot be
// computed, only Reason is set.
type SeqcolValue struct {
	Digest          string `json:"digest,omitempty"`
	LabelsDigest    string `json:"names_digest,omitempty"`
	LengthsDigest   string `json:"lengths_digest,omitempty"`
	SequencesDigest string `json:"sequences_digest,omitempty"`
	Sequences       int    `json:"sequences,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// Fact ends the parse and returns the seqcol fact. A file that has no
// collection is not an error: the fact says why.
func (c *Collector) Fact() (model.Fact, error) {
	value := SeqcolValue{}
	collection, err := c.Collection()
	if err == nil {
		var d Digests
		if d, err = collection.Digest(); err == nil {
			value = SeqcolValue{
				Digest: d.Collection, LabelsDigest: d.Labels, LengthsDigest: d.Lengths,
				SequencesDigest: d.Sequences, Sequences: len(collection.Names),
			}
		}
	}
	if err != nil {
		value = SeqcolValue{Reason: err.Error()}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return model.Fact{}, err
	}
	return model.Fact{Kind: FactKind, ExtractorVersion: ExtractorVersion, Value: raw}, nil
}
