package neovim

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/loft-sh/devpod/pkg/command"
	"github.com/loft-sh/devpod/pkg/config"
	copy2 "github.com/loft-sh/devpod/pkg/copy"
	devpodhttp "github.com/loft-sh/devpod/pkg/http"
	"github.com/loft-sh/devpod/pkg/ide"
	"github.com/loft-sh/devpod/pkg/single"
	"github.com/loft-sh/log"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
)

const DefaultNeovimPort = 9251
const DownloadNvimTemplate = "https://github.com/neovim/neovim/releases/%s/download/nvim.appimage"

const (
	VersionOption      = "VERSION"
	ForwardPortsOption = "FORWARD_PORTS"
	OpenOption         = "OPEN"
	BindAddressOption  = "BIND_ADDRESS"
	ConfigDirectory    = "CONFIG_DIRECTORY"
)

var Options = ide.Options{
	VersionOption: {
		Name:        VersionOption,
		Description: "The version for the neovim binary",
		Default:     "latest",
	},
	ForwardPortsOption: {
		Name:        ForwardPortsOption,
		Description: "If DevPod should automatically do port-forwarding",
		Default:     "true",
		Enum: []string{
			"true",
			"false",
		},
	},
	BindAddressOption: {
		Name:        BindAddressOption,
		Description: "The address to bind Neovim to locally. E.g. 0.0.0.0:12345",
		Default:     "",
	},
	OpenOption: {
		Name:        OpenOption,
		Description: "If DevPod should automatically open Neovim",
		Default:     "true",
		Enum: []string{
			"true",
			"false",
		},
	},
	ConfigDirectory: {
		Name:        ConfigDirectory,
		Description: "Config directory for Neovim",
		Default:     "",
	},
}

func NewNeovimServer(userName string, host string, port string, values map[string]config.OptionValue, log log.Logger) *NeovimServer {
	return &NeovimServer{
		values:   values,
		host:     host,
		port:     port,
		userName: userName,
		log:      log,
	}
}

type NeovimServer struct {
	values   map[string]config.OptionValue
	host     string
	port     string
	userName string
	log      log.Logger
}

func (o *NeovimServer) Install() error {
	writer := o.log.Writer(logrus.InfoLevel, false)
	defer writer.Close()

	o.log.Infof("Checking if Neovim exists...")
	location, err := prepareNeovimServerLocation(o.userName)
	if err != nil {
		return err
	}

	// is installed
	_, err = exec.LookPath("nvim")
	if err == nil {
		return nil
	}

	o.log.Infof("Installing Neovim...")
	// check what release we need to download
	var url string
	version := Options.GetValue(o.values, VersionOption)
	if url == "" {
		url = fmt.Sprintf(DownloadNvimTemplate, version)
	}

	// Download neovim appimage
	resp, err := devpodhttp.GetHTTPClient().Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	appImageFile := filepath.Join(location, "nvim.appimage")
	file, err := os.Create(appImageFile)
	if err != nil {
		return err
	}

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return errors.Wrap(err, "download Neovim")
	}
	file.Close()

	// Extract and setup Nvim binary
	commands := [][]string{{"chmod", "u+x", appImageFile}, {appImageFile, "--appimage-extract"}, {"rm", appImageFile}, {"mv", "squashfs-root", location}, {"ln", "-sf", fmt.Sprintf("%s/squashfs-root/AppRun", location), "/usr/bin/nvim"}}
	for _, command := range commands {
		cmd := exec.Command(command[0], command[1:]...)
		cmd.Stderr = writer
		err = cmd.Run()
		if err != nil {
			return errors.Wrap(err, "extracting Neovim")
		}
	}

	// Chown location
	if o.userName != "" {
		err = copy2.Chown(location, o.userName)
		if err != nil {
			return errors.Wrap(err, "chown")
		}
	}

	o.log.Infof("Successfully installed neovim")
	return nil
}

func (o *NeovimServer) Start(workspaceFolder string) error {
	_, err := prepareNeovimServerLocation(o.userName)
	if err != nil {
		return err
	}

	if o.host == "" {
		o.host = "0.0.0.0"
	}
	if o.port == "" {
		o.port = strconv.Itoa(DefaultNeovimPort)
	}

	return single.Single("neovim.pid", func() (*exec.Cmd, error) {
		o.log.Infof("Starting Neovim in background...")
		runCommand := fmt.Sprintf("nvim --listen %s:%s --headless", o.host, o.port)
		args := []string{}
		if o.userName != "" {
			args = append(args, "su", o.userName, "-c", runCommand)
		} else {
			args = append(args, "sh", "-c", runCommand)
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = workspaceFolder
		return cmd, nil
	})
}

func prepareNeovimServerLocation(userName string) (string, error) {
	var err error
	homeFolder := ""
	if userName != "" {
		homeFolder, err = command.GetHome(userName)
	} else {
		homeFolder, err = homedir.Dir()
	}
	if err != nil {
		return "", err
	}

	folder := filepath.Join(homeFolder, "nvim")
	err = os.MkdirAll(folder, 0777)
	if err != nil {
		return "", err
	}

	return folder, nil
}
