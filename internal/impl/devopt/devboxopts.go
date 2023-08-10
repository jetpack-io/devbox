package devopt

import (
	"io"
)

type Opts struct {
	AllowInsecureAdds        bool
	Dir                      string
	Env                      map[string]string
	Pure                     bool
	IgnoreWarnings           bool
	CustomProcessComposeFile string
	Writer                   io.Writer
}

type EnvFlags struct {
	EnvMap  map[string]string
	EnvFile string
}
