// Package dockercompose encapsulates docker compose commands so that we can spin up instances for testing
// The main thing this does is manage the life cycle of the services, and when a service can be shutdown
// and brought up.
package dockercompose

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
)

var docker cli

// PS returns the status of all the services in the docker-compose yml file
func PS() ([]ProcessStatus, error) {
	return docker.PS(context.Background())
}

// Start a single service that is defined in the docker-compose yml file
// returns any port definitions as well
func Start(service string) (ports PSPorts, err error) {
	if service == "" {
		return nil, ErrServiceNameRequired
	}
	sStatus := getServiceStatus(service)
	notRunning := sStatus.IsZero() || !sStatus.Running
	if notRunning {
		// we need to bring up the container in order to start up the service
		sStatus, err = Up(service)
		if err != nil {
			return nil, err
		}
	}

	// need to look for: service "minio" has no container to start
	// if it is found need to bring up the container
	// service was already running
	runningServicesLock.Lock()
	runningServices[service] = runningServices[service].Increment(sStatus, !notRunning)
	runningServicesLock.Unlock()
	return sStatus.Ports, nil
}

func Stop(service string) error {
	if service == "" {
		return ErrServiceNameRequired
	}
	sStatus := getServiceStatus(service)
	if sStatus.IsZero() || !sStatus.Running {
		// service is not running
		runningServicesLock.Lock()
		s := runningServices[service]
		s.count = 1 // make sure count is one so that Decrement does the right things for closing things out
		runningServices[service] = s.Decrement()
		runningServicesLock.Unlock()
		return nil
	}
	runningServicesLock.Lock()
	s := runningServices[service].Decrement()
	runningServices[service] = s
	runningServicesLock.Unlock()
	if s.Teardown() {
		if err := docker.Stop(context.Background(), service); err != nil {
			return ErrFailedToStopService{
				Service: service,
				Err:     err,
			}
		}
	}
	return nil
}

// Up will bring up and start a service, it will not add the instance to the accounting of which services have
// been brought up, that will be the responsibility of the calling application
func Up(service string) (ProcessStatus, error) {
	if err := docker.Up(context.Background(), service); err != nil {
		return ProcessStatus{}, ErrFailedToStartService{
			Service: service,
			Err:     err,
		}
	}

	status := getServiceStatus(service)
	if status.IsZero() {
		return status, ErrFailedToStartService{
			Service: service,
		}
	}
	return status, nil
}

type serviceStatus struct {
	ProcessStatus
	count uint32
}

func (ss serviceStatus) String() string {
	return fmt.Sprintf("service %v (+%d) : %v", ss.Service, ss.count, ss.Status)
}

func (ss serviceStatus) Increment(ps ProcessStatus, alreadyRunning bool) serviceStatus {
	if ss.count == 0 && alreadyRunning {
		ss.count++
	}
	ss.count++
	ss.ProcessStatus = ps
	return ss
}

func (ss serviceStatus) Decrement() serviceStatus {

	if ss.count == 0 {
		return ss
	}
	ss.count--
	if ss.count == 0 {
		ss.ProcessStatus = ProcessStatus{}
	}
	return ss
}

func (ss serviceStatus) Teardown() bool {
	return ss.count == 0
}

var (
	runningServicesLock sync.RWMutex
	runningServices     = make(map[string]serviceStatus)
)

type PSPort struct {
	IPAddress net.IP // the IP address, this could be ipv6 or ipv4, or nil if internal only
	Internal  uint64 // internal port number
	External  uint64 // if 0 the address is internal only
	// Is the protocol tcp
	UDP bool
}

func (port PSPort) ExternalHost() string {
	var buf strings.Builder
	buf.WriteString(port.IPAddress.String())
	buf.WriteByte(':')
	buf.WriteString(strconv.FormatUint(port.External, 10))
	return buf.String()
}

func (port PSPort) String() string {
	var buf strings.Builder
	if port.External != 0 {
		buf.WriteString(port.ExternalHost())
		buf.WriteString("->")
	}
	buf.WriteString(strconv.FormatUint(port.Internal, 10))
	if port.UDP {
		buf.WriteString("/UDP")
	} else {
		buf.WriteString("/TCP")
	}
	return buf.String()
}

func (port PSPort) IsInternal() bool {
	return port.External == 0
}
func (port PSPort) IsIPv4() bool {
	return port.IPAddress.To4() != nil
}
func (port PSPort) IsZero() bool {
	return port.Internal == 0
}

type PSPorts []PSPort

func (ports PSPorts) FindPort(internalPortNumber uint64, ipv4, udp bool) PSPort {
	if internalPortNumber == 0 {
		return PSPort{}
	}
	for _, port := range ports {
		if port.IsInternal() || port.UDP != udp || port.IsIPv4() != ipv4 || port.Internal != internalPortNumber {
			continue
		}
		return port
	}
	return PSPort{}
}

type ProcessStatus struct {
	// Name of the services after starting up, will usually only be service_name-1 unless there is a cluster setup
	Name string
	// Command that the services was started with
	Command string
	// Service is the service name
	Service string
	// Status is the current status of the services, running, stopped, etc...
	Status string
	// State of the service, e.g. healthy, etc...
	State string
	// Health is the health
	Health string
	// Running is the service running
	Running bool
	// Ports are the exposed and unexposed port of the service
	Ports []PSPort
}

func (ps ProcessStatus) IsZero() bool {
	return ps.Name == "" && ps.Command == "" && ps.Status == "" && len(ps.Ports) == 0
}
func (ps ProcessStatus) IsHealthy() bool {
	return ps.Health == "healthy"
}

type ByProcessService []ProcessStatus

func (ps ByProcessService) Len() int           { return len(ps) }
func (ps ByProcessService) Less(i, j int) bool { return ps[i].Service < ps[j].Service }
func (ps ByProcessService) Swap(i, j int)      { ps[i], ps[j] = ps[j], ps[i] }

func FindServiceIdx(statuses []ProcessStatus, service string) int {
	for i := range statuses {
		if statuses[i].Service == service {
			return i
		}
	}
	return -1
}

func updateProcessStatuses(statuses []ProcessStatus) {
	newRunning := make(map[string]serviceStatus, len(statuses))
	for i := range statuses {
		if !statuses[i].Running {
			continue
		}
		newRunning[statuses[i].Service] = serviceStatus{
			count:         1,
			ProcessStatus: statuses[i],
		}
	}
	runningServicesLock.Lock()
	runningServices = newRunning
	runningServicesLock.Unlock()
}

func getServiceStatus(service string) ProcessStatus {
	statuses, err := PS()
	if err != nil {
		return ProcessStatus{Service: service}
	}
	idx := FindServiceIdx(statuses, service)
	if idx == -1 {
		return ProcessStatus{Service: service}
	}
	return statuses[idx]
}

func getRunningServiceStatus(service string) serviceStatus {
	runningServicesLock.RLock()
	defer runningServicesLock.RUnlock()
	return runningServices[service]
}
