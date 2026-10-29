package model

type Severity string

const (
	Fatal    Severity = "fatal"
	Blocking Severity = "blocking"
	Advisory Severity = "advisory"
)

func (s Severity) Rank() int {
	switch s {
	case Fatal:
		return 3
	case Blocking:
		return 2
	case Advisory:
		return 1
	}
	return 0
}
