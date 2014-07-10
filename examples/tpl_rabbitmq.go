package main

import "github.com/dynport/urknall"

func NewRabbitMQ(version string) *RabbitMQ {
	return &RabbitMQ{Version: version}
}

type RabbitMQ struct {
	Version string `urknall:"default=3.2.1"`
}

func (p *RabbitMQ) Render(r urknall.Package) {
	r.AddCommands("base",
		InstallPackages("erlang-nox", "erlang-reltool", "erlang-dev"),
		Mkdir("/opt/src/", "root", 0755),
		DownloadAndExtract(p.url(), "/opt/"),
		Shell("cd {{ .InstallPath }} && ./sbin/rabbitmq-plugins enable rabbitmq_management"),
		WriteFile("/etc/init/rabbitmq.conf", "env HOME=/root\nexec {{ .InstallPath }}/sbin/rabbitmq-server\n", "root", 0644),
	)
}

func (p *RabbitMQ) InstallPath() string {
	return "/opt/rabbitmq_server-{{ .Version }}"
}

func (p *RabbitMQ) url() string {
	return "http://www.rabbitmq.com/releases/rabbitmq-server/v{{ .Version }}/rabbitmq-server-generic-unix-{{ .Version }}.tar.gz"
}