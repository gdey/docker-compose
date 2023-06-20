package dockercompose

import (
	"context"
	"errors"
	"net"
	"sort"
	"sync"

	"github.com/compose-spec/compose-go/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	ccompose "github.com/docker/compose/v2/cmd/compose"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/compose/v2/pkg/compose"
)

type cli struct {
	service     api.Service
	projectLock sync.Mutex
	project     *types.Project

	init *sync.Once
}

// PS will get the process status for the given services, or all the services if none is Provided
func (c *cli) PS(ctx context.Context, services ...string) ([]ProcessStatus, error) {
	if err := c.initialize(); err != nil {
		return nil, err
	}
	var (
		ops = api.PsOptions{
			All:      len(services) == 0,
			Services: services,
		}
	)
	c.projectLock.Lock()
	defer c.projectLock.Unlock()
	containers, err := c.service.Ps(ctx, c.project.Name, ops)
	if err != nil {
		return nil, err
	}
	return mapContainerSummariesToProcessStatuses(containers), nil
}

// Stop will stop a service or service if they are running
func (c *cli) Stop(ctx context.Context, services ...string) error {
	if err := c.initialize(); err != nil {
		return err
	}

	opts := api.StopOptions{
		Services: services,
	}
	c.projectLock.Lock()
	defer c.projectLock.Unlock()
	return c.service.Stop(ctx, c.project.Name, opts)
}

// Up will bring up a service or services if they are not already running
func (c *cli) Up(ctx context.Context, services ...string) error {
	if err := c.initialize(); err != nil {
		return err
	}
	// first let's see if the service exists
	pStatuses, err := c.PS(ctx)
	if err != nil {
		return err
	}
	var (
		servicesToCreate []string
		servicesToStart  []string
	)
	c.projectLock.Lock()
	defer c.projectLock.Unlock()
NextService:
	for i := range services {
		for _, status := range pStatuses {
			if status.Service == services[i] {
				if !status.Running {
					servicesToStart = append(servicesToStart, status.Service)
				}
				continue NextService
			}
		}
		servicesToCreate = append(servicesToCreate, services[i])
		servicesToStart = append(servicesToStart, services[i])
	}
	if len(servicesToCreate) == 0 && len(servicesToStart) == 0 {
		return nil
	}
	if len(servicesToCreate) > 0 {
		err := c.service.Create(
			ctx,
			filterProjectToServices(c.project, servicesToCreate...),
			api.CreateOptions{Services: servicesToCreate, QuietPull: true},
		)
		if err != nil {
			return err
		}
	}
	if len(servicesToStart) > 0 {
		err := c.service.Start(ctx, c.project.Name, api.StartOptions{
			Project:      filterProjectToServices(c.project, servicesToStart...),
			Wait:         true,
			Services:     servicesToStart,
			ExitCodeFrom: servicesToStart[0],
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Start will start a service or services if they are not already running
func (c *cli) Start(ctx context.Context, services ...string) error {
	if err := c.initialize(); err != nil {
		return err
	}
	opts := api.StartOptions{
		Project:     filterProjectToServices(c.project, services...),
		CascadeStop: false,
		Wait:        true,
		Services:    services,
	}
	c.projectLock.Lock()
	defer c.projectLock.Unlock()
	return c.service.Start(ctx, "", opts)
}

func (c *cli) KnownServices() ([]string, error) {
	if err := c.initialize(); err != nil {
		return nil, err
	}
	c.projectLock.Lock()
	defer c.projectLock.Unlock()
	services := make([]string, 0, len(c.project.Services))
	for _, srv := range c.project.Services {
		services = append(services, srv.Name)
	}
	return services, nil
}

func (c *cli) getProjectName() error {
	o := ccompose.ProjectOptions{}
	prj, err := o.ToProject(nil)
	if err != nil {
		return err
	}
	c.project = prj
	return nil
}
func (c *cli) client() error {
	opts := flags.NewClientOptions()
	client, err := command.NewDockerCli()
	if err != nil {
		return err
	}
	if err = client.Initialize(opts); err != nil {
		return err
	}
	c.service = compose.NewComposeService(client)

	return err
}

func (c *cli) initialize() (err error) {
	if c == nil {
		return errors.New("cli is nil")
	}
	if c.init == nil {
		c.init = new(sync.Once)
	}
	c.init.Do(func() {
		if err = c.getProjectName(); err != nil {
			return
		}
		if err = c.client(); err != nil {
			return
		}
	})
	if err != nil {
		c.init = nil
	}
	return err
}

func mapContainerSummaryToProcessStatus(c api.ContainerSummary) ProcessStatus {
	ps := ProcessStatus{
		Name:    c.Name,
		Command: c.Command,
		Service: c.Service,
		Status:  c.Status,
		State:   c.State,
		Health:  c.Health,
		Running: c.State == "running" && c.Health == "healthy",
	}
	if len(c.Publishers) > 0 {
		ps.Ports = make([]PSPort, 0, len(c.Publishers))
		for _, port := range c.Publishers {
			ps.Ports = append(ps.Ports, PSPort{
				IPAddress: net.ParseIP(port.URL),
				Internal:  uint64(port.TargetPort),
				External:  uint64(port.PublishedPort),
				UDP:       port.Protocol == "udp",
			})
		}
	}
	return ps
}
func mapContainerSummariesToProcessStatuses(containers []api.ContainerSummary) (ps []ProcessStatus) {
	ps = make([]ProcessStatus, len(containers))
	for i := range containers {
		ps[i] = mapContainerSummaryToProcessStatus(containers[i])
	}
	sort.Sort(ByProcessService(ps))
	return ps
}

func servicesWithDependencies(p *types.Project, services ...string) []string {
	if p == nil {
		return nil
	}
	depends := make(map[string]struct{}, len(services))
	for _, name := range services {
		for i := range p.Services {
			if p.Services[i].Name == name {
				depends[name] = struct{}{}
				for _, name := range p.Services[i].GetDependencies() {
					depends[name] = struct{}{}
				}
			}
		}
	}
	var srv []string
	for name := range depends {
		srv = append(srv, name)
	}
	sort.Strings(srv)
	return srv
}

func filterProjectToServices(p *types.Project, services ...string) *types.Project {
	if p == nil {
		return nil
	}
	var pp = *p
	services = servicesWithDependencies(p, services...)
	pp.Services = make(types.Services, 0, len(services))
LoopServices:
	for i := range p.Services {
		for _, name := range services {
			if p.Services[i].Name != name {
				continue
			}
			pp.Services = append(pp.Services, p.Services[i])
			continue LoopServices
		}
	}
	return &pp
}
