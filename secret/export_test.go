package secret

import "net/url"

func RestrictedNameForTest(raw, opaque string) Name {
	return Name{raw: raw, opaque: opaque, restricted: true}
}

func RestrictedEndpointForTest(raw string) Endpoint {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return Endpoint{u: u, restricted: true}
}

func SecretForTest(v string) Secret { return Secret{v: v} }
