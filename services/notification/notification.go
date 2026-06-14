package notification

type Confirmation struct {
	Email string
	Token string `json:"-"`
	Repo  string
}

type ReleaseInfo struct {
	TagName     string
	Name        string
	HTMLURL     string
	PublishedAt string
}
