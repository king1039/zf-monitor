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

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
)

type ProcessInfo struct {
	PID       int32   `json:"pid"`
	Name      string  `json:"name"`
	CPU       float64 `json:"cpu"`
	MemoryMB  float64 `json:"memoryMB"`
}

type Report struct {
	Hostname string       `json:"hostname"`
	Timestamp int64       `json:"timestamp"`
	CPU       float64      `json:"cpu"`
	Memory    float64      `json:"memory"`
	Disk      float64      `json:"disk"`
	NetUp     float64      `json:"netUp"`
	NetDown   float64      `json:"netDown"`
	Processes []ProcessInfo `json:"processes"`
}

var (
	netLastSent  uint64
	netLastRecv  uint64
	netLastTime  time.Time
)

func main() {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}

	for {
		report, err := collectReport(hostname)
		if err != nil {
			log.Printf("collect report failed: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		if err := sendReport(report); err != nil {
			log.Printf("send report failed: %v", err)
		} else {
			log.Printf("report sent host=%s cpu=%.1f memory=%.1f", report.Hostname, report.CPU, report.Memory)
		}

		time.Sleep(5 * time.Second)
	}
}

func collectReport(hostname string) (Report, error) {
	report := Report{
		Hostname: hostname,
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

func sendReport(report Report) error {
	payload, err := json.Marshal(report)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post("http://172.16.176.202:8080/api/report", "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("status %s", resp.Status)
	}

	return nil
}
