package navidrome

// Song represents a Navidrome track.
type Song struct {
	ID        string  `json:"id"`
	Title     string  `json:"title"`
	Artist    string  `json:"artist"`
	Album     string  `json:"album"`
	Duration  float64 `json:"duration"`
	Path      string  `json:"path"`      // absolute path on Navidrome's filesystem
	HasLyrics bool    `json:"hasLyrics"` // true if Navidrome found any lyrics
}

type authRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	Token string `json:"token"`
}
