package syslogng

import (
	"github.com/dynport/urknall"
	"github.com/dynport/urknall/cmd"
)

func New(version string) *Package {
	return &Package{Version: version}
}

type Package struct {
	Version string `urknall:"default=3.5.1"`
}

func (ng *Package) url() string {
	return "http://www.balabit.com/downloads/files/syslog-ng/open-source-edition/{{ .Version }}/source/syslog-ng_{{ .Version }}.tar.gz"
}

func (ng *Package) Package(r *urknall.Runlist) {
	r.Add(
		cmd.InstallPackages("build-essential", "libevtlog-dev", "pkg-config", "libglib2.0-dev"),
		cmd.DownloadAndExtract(ng.url(), "/opt/src"),
		cmd.And(
			"cd /opt/src/syslog-ng-{{ .Version }}",
			"./configure",
			"make",
			"make install",
		),
		cmd.WriteFile("/etc/init/syslog-ng.conf", UpstartScript, "root", 0644),
	)
}

const UpstartScript = `# syslog-ng - system logging daemon
#
# syslog-ng is an replacement for the traditionala syslog daemon, logging messages from applications

description     "system logging daemon"

start on filesystem
stop on runlevel [06]

env LD_LIBRARY_PATH=/usr/local/lib

expect fork
respawn

exec syslog-ng --process-mode=background
`