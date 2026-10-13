package conns

type TagConfig struct {
	DefaultTags       map[string]string
	IgnoreKeys        []string
	IgnoreKeyPrefixes []string
}
