package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"golang.org/x/sys/windows/svc"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

const (
	serviceName        = "ZFMonitorAgent"
	serviceDisplayName = "ZF Monitor Agent"
)

type ProcessInfo struct {
	PID      int32   `json:"pid"`
	Name     string  `json:"name"`
	CPU      float64 `json:"cpu"`
	MemoryMB float64 `json:"memoryMB"`
}

type Report struct {
	HostID    string        `json:"hostId"`
	Hostname  string        `json:"hostname"`
	Timestamp int64         `json:"timestamp"`
	CPU       float64       `json:"cpu"`
	Memory    float64       `json:"memory"`
	Disk      float64       `json:"disk"`
	NetUp     float64       `json:"netUp"`
	NetDown   float64       `json:"netDown"`
	Processes []ProcessInfo `json:"processes"`
}

const defaultServerURL = "http://172.16.176.202:8080"

var (
	netLastSent uint64
	netLastRecv uint64
	netLastTime time.Time
)

func main() {
	isService, err := svc.IsWindowsService()
	if err != nil {
		log.Printf("svc.IsWindowsService() failed: %v", err)
		isService = false
	}

	if isService {
		if err := svc.Run(serviceName, &agentService{}); err != nil {
			log.Fatalf("failed to run %s service: %v", serviceName, err)
		}
		return
	}

	runAgentLoop(nil)
}

type agentService struct{}

func (m *agentService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}

	stopCh := make(chan struct{})
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		runAgentLoop(stopCh)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				close(stopCh)
				<-doneCh
				changes <- svc.Status{State: svc.Stopped}
				return false, 0
			default:
				log.Printf("service request %d ignored", c.Cmd)
			}
		case <-doneCh:
			return false, 0
		}
	}
}

func runAgentLoop(stopCh <-chan struct{}) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}
	hostID := hostname
	for {
		select {
		case <-stopCh:
			log.Printf("agent stop requested; exiting collection loop")
			return
		default:
		}

		report, err := collectReport(hostname, hostID)
		if err != nil {
			log.Printf("collect report failed: %v", err)
		} else {
			if err := sendReport(report, defaultServerURL); err != nil {
				log.Printf("send report failed: %v", err)
			} else {
				log.Printf("report sent host=%s cpu=%.1f memory=%.1f", report.Hostname, report.CPU, report.Memory)
			}
		}

		select {
		case <-stopCh:
			log.Printf("agent stop requested; exiting collection loop")
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func collectReport(hostname, hostID string) (Report, error) {
	report := Report{
		HostID:    hostID,
		Hostname:  hostname,
		Timestamp: time.Now().Unix(),
	}

	cpuPercents, err := cpu.Percent(500*time.Millisecond, false)
	if err == nil && len(cpuPercents) > 0 {
		report.CPU = cpuPercents[0]
	}

	vm, err := mem.VirtualMemory()
	if err == nil {
		report.Memory = vm.UsedPercent
	}

	path := "C:\\"
	usage, err := disk.Usage(path)
	if err == nil {
		report.Disk = usage.UsedPercent
	}

	report.NetUp, report.NetDown = collectNetworkRate()
	report.Processes = collectProcesses()

	return report, nil
}

func collectProcesses() []ProcessInfo {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}

	infos := make([]ProcessInfo, 0, 8)
	for _, p := range procs {
		if p == nil {
			continue
		}

		name, _ := p.Name()
		pid := p.Pid
		cpuPct, _ := p.CPUPercent()
		memInfo, _ := p.MemoryInfo()

		if name == "" {
			continue
		}
		if cpuPct < 0 {
			cpuPct = 0
		}

		memMB := 0.0
		if memInfo != nil {
			memMB = float64(memInfo.RSS) / 1024 / 1024
		}

		infos = append(infos, ProcessInfo{
			PID:      int32(pid),
			Name:     name,
			CPU:      cpuPct,
			MemoryMB: memMB,
		})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].CPU > infos[j].CPU
	})
	if len(infos) > 8 {
		infos = infos[:8]
	}
	return infos
}

func collectNetworkRate() (float64, float64) {
	counters, err := net.IOCounters(true)
	if err != nil {
		return 0, 0
	}

	var totalSent, totalRecv uint64
	for _, counter := range counters {
		totalSent += counter.BytesSent
		totalRecv += counter.BytesRecv
	}

	now := time.Now()
	if netLastTime.IsZero() {
		netLastSent = totalSent
		netLastRecv = totalRecv
		netLastTime = now
		return 0, 0
	}

	elapsed := now.Sub(netLastTime).Seconds()
	if elapsed <= 0 {
		return 0, 0
	}

	up := float64(totalSent-netLastSent) / elapsed
	down := float64(totalRecv-netLastRecv) / elapsed

	netLastSent = totalSent
	netLastRecv = totalRecv
	netLastTime = now

	return up, down
}

func sendReport(report Report, serverURL string) error {
	payload, err := json.Marshal(report)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(serverURL+"/api/report", "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("status %s", resp.Status)
	}

	return nil
}
