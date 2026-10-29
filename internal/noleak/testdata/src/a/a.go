package a

type Name struct{ raw string }

func (n Name) Raw() string { return n.raw }

func Reveal() string { return "" }

type Artifact struct {
	Name     string // want `field/parameter "Name" is a raw string where the domain requires secret\.Name, secret\.Endpoint or secret\.Secret`
	FilePath string // want `field/parameter "FilePath" is a raw string where the domain requires secret\.Name, secret\.Endpoint or secret\.Secret`
	URL      string // want `field/parameter "URL" is a raw string where the domain requires secret\.Name, secret\.Endpoint or secret\.Secret`
	Comment  string
	N        Name
}

func Fetch(token string) { // want `field/parameter "token" is a raw string where the domain requires secret\.Name, secret\.Endpoint or secret\.Secret`
	_ = token
}

func useRaw(n Name) string {
	return n.Raw() // want `call to n\.Raw outside internal/fsaccess or transport/`
}

func useReveal() string {
	return Reveal() // want `call to Reveal outside internal/fsaccess or transport/`
}
