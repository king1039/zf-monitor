# ZF Monitor

A minimal server monitoring system with two independent Go projects in one repository.

## Structure

```text
zf-monitor/
├── zf-monitor-agent/
│   ├── go.mod
│   ├── main.go
│   └── dist/
│       └── zf-monitor-agent.exe
├── zf-monitor-back/
│   ├── go.mod
│   ├── main.go
│   ├── data/
│   └── web/
├── .gitignore
├── README.md
└── .github/
```

## Components

### zf-monitor-agent
Runs on Windows Server 2022.

Responsibilities:
- collect CPU, memory, disk, network, process metrics
- send JSON report every 5 seconds
- POST to http://172.16.176.202:8080/api/report

Build:

```bash
go build -o zf-monitor-agent.exe
```

Or cross-compile for Windows:

```bash
GOOS=windows GOARCH=amd64 go build -o dist/zf-monitor-agent.exe
```

### zf-monitor-back
Runs on Ubuntu Linux.

Responsibilities:
- receive agent reports
- save metrics to SQLite
- simple alerting
- expose API and dashboard

Build:

```bash
go build -o zf-monitor-back
```

Run:

```bash
./zf-monitor-back
```

## Default backend URL

```text
http://172.16.176.202:8080
```

## Dashboard

Open:

```text
http://8.148.227.2:8080/
```

## GitHub release for agent

Generate the Windows executable and attach it to a GitHub Release as a binary asset:

```bash
cd zf-monitor-agent
mkdir -p dist
GOOS=windows GOARCH=amd64 go build -o dist/zf-monitor-agent.exe
```

Then publish the file as a release artifact.

Windows servers can download it via:

```text
https://github.com/<your-user>/<your-repo>/releases/latest/download/zf-monitor-agent.exe
```
