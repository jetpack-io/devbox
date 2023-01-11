package plugin

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/samber/lo"
	"go.jetpack.io/devbox/internal/boxcli/usererr"
)

type Services map[string]service

type service struct {
	Name  string `json:"name"`
	Start string `json:"start"`
	Stop  string `json:"stop"`
}

func GetServices(pkgs []string, projectDir string) (Services, error) {
	services := map[string]service{}
	for _, pkg := range pkgs {
		c, err := getConfigIfAny(pkg, projectDir)
		if err != nil {
			return nil, err
		}
		if c == nil {
			continue
		}
		for name, svc := range c.Services {
			svc.Name = name
			services[name] = svc
		}
	}
	return services, nil
}

func (s *Services) UnmarshalJSON(b []byte) error {
	var m map[string]service
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*s = make(Services)
	for name, svc := range m {
		svc.Name = name
		(*s)[name] = svc
	}
	return nil
}

func StartServices(pkgs, serviceNames []string, root string, w io.Writer) error {
	return toggleServices(pkgs, serviceNames, root, w, startService)
}

func StopServices(pkgs, serviceNames []string, root string, w io.Writer) error {
	return toggleServices(pkgs, serviceNames, root, w, stopService)
}

type serviceAction int

const (
	startService serviceAction = iota
	stopService
)

func toggleServices(
	pkgs,
	serviceNames []string,
	root string,
	w io.Writer,
	action serviceAction,
) error {
	services, err := GetServices(pkgs, root)
	if err != nil {
		return err
	}
	envVars, err := Env(pkgs, root)
	if err != nil {
		return err
	}
	for _, name := range serviceNames {
		service, found := services[name]
		if !found {
			return usererr.New("Service not found")
		}
		cmd := exec.Command(
			"sh",
			"-c",
			lo.Ternary(action == startService, service.Start, service.Stop),
		)
		cmd.Stdout = w
		cmd.Stderr = w
		cmd.Env = envVars
		cmd.Env = append(cmd.Env, os.Environ()...)
		if err = cmd.Run(); err != nil {
			actionString := lo.Ternary(action == startService, "start", "stop")
			if len(serviceNames) == 1 {
				return usererr.WithUserMessage(err, "Service %q failed to %s", name, actionString)
			}
			fmt.Fprintf(w, "Service %q failed to %s. Error = %s\n", name, actionString, err)
		} else {
			actionStringPast := lo.Ternary(action == startService, "started", "stopped")
			fmt.Fprintf(w, "Service %q %s\n", name, actionStringPast)
		}
	}
	return nil
}
