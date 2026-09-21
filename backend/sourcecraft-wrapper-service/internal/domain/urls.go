package domain

type CloneURL struct {
	HttpURL string `json:"https"`
	SshURL  string `json:"ssh"`
}
