// Copyright 2017 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cloudfoundry

import (
	"errors"
	"fmt"

	copilotapi "code.cloudfoundry.org/copilot/api"
	"golang.org/x/net/context"

	"istio.io/istio/pilot/pkg/model"
)

//go:generate counterfeiter -o ./fakes/copilot_client.go --fake-name CopilotClient . copilotClient
// CopilotClient defines a local interface for interacting with Cloud Foundry Copilot
type copilotClient interface {
	copilotapi.IstioCopilotClient
}

// ServiceDiscovery implements the model.ServiceDiscovery interface for Cloud Foundry
type ServiceDiscovery struct {
	Client copilotClient

	// Cloud Foundry currently only supports applications exposing a single HTTP or TCP port
	// It is typically 8080
	ServicePort int
}

// Services implements a service catalog operation
func (sd *ServiceDiscovery) Services() ([]*model.Service, error) {
	resp, err := sd.Client.Routes(context.Background(), new(copilotapi.RoutesRequest))
	if err != nil {
		return nil, fmt.Errorf("getting services: %s", err)
	}
	services := make([]*model.Service, 0, len(resp.GetBackends()))

	port := sd.servicePort()
	for hostname := range resp.Backends {
		services = append(services, &model.Service{
			Hostname:     model.Hostname(hostname),
			Ports:        []*model.Port{port},
			MeshExternal: false,
			Resolution:   model.ClientSideLB,
		})
	}

	internalRoutesResp, err := sd.Client.InternalRoutes(context.Background(), new(copilotapi.InternalRoutesRequest))
	if err != nil {
		return nil, fmt.Errorf("getting services: %s", err)
	}

	internalRouteServicePort := &model.Port{
		Port:     sd.ServicePort,
		Protocol: model.ProtocolTCP,
		Name:     "tcp",
	}

	for _, internalRoute := range internalRoutesResp.GetInternalRoutes() {
		services = append(services, &model.Service{
			Hostname:     model.Hostname(internalRoute.Hostname),
			Address:      internalRoute.Vip,
			Ports:        []*model.Port{internalRouteServicePort},
			MeshExternal: false,
			Resolution:   model.ClientSideLB,
		})
	}

	return services, nil
}

// GetService implements a service catalog operation
func (sd *ServiceDiscovery) GetService(hostname model.Hostname) (*model.Service, error) {
	services, err := sd.Services()
	if err != nil {
		return nil, err
	}
	for _, svc := range services {
		if svc.Hostname == hostname {
			return svc, nil
		}
	}
	return nil, nil
}

// Instances implements a service catalog operation
func (sd *ServiceDiscovery) Instances(hostname model.Hostname, _ []string, _ model.LabelsCollection) ([]*model.ServiceInstance, error) {
	return nil, errors.New("not implemented. use InstancesByPort instead")
}

// InstancesByPort implements a service catalog operation
func (sd *ServiceDiscovery) InstancesByPort(hostname model.Hostname, _ []int, _ model.LabelsCollection) ([]*model.ServiceInstance, error) {
	resp, err := sd.Client.Routes(context.Background(), new(copilotapi.RoutesRequest))
	if err != nil {
		return nil, fmt.Errorf("getting routes: %s", err)
	}
	instances := make([]*model.ServiceInstance, 0)
	backendSet := resp.GetBackends()[hostname.String()]
	for _, backend := range backendSet.GetBackends() {
		port := sd.servicePort()

		instances = append(instances, &model.ServiceInstance{
			Endpoint: model.NetworkEndpoint{
				Address:     backend.Address,
				Port:        int(backend.Port),
				ServicePort: port,
			},
			Service: &model.Service{
				Hostname:     hostname,
				Ports:        []*model.Port{port},
				MeshExternal: false,
				Resolution:   model.ClientSideLB,
			},
		})
	}

	internalRoutesResp, err := sd.Client.InternalRoutes(context.Background(), new(copilotapi.InternalRoutesRequest))
	if err != nil {
		return nil, fmt.Errorf("getting internal routes: %s", err)
	}

	internalRouteServicePort := &model.Port{
		Port:     sd.ServicePort,
		Protocol: model.ProtocolTCP,
		Name:     "tcp",
	}

	for _, internalRoute := range internalRoutesResp.GetInternalRoutes() {
		for _, backend := range internalRoute.GetBackends().Backends {
			if internalRoute.Hostname == hostname.String() {
				instances = append(instances, &model.ServiceInstance{
					Endpoint: model.NetworkEndpoint{
						Address:     backend.Address,
						Port:        int(backend.Port),
						ServicePort: internalRouteServicePort,
					},
					Service: &model.Service{
						Hostname:     hostname,
						Address:      internalRoute.Vip,
						Ports:        []*model.Port{internalRouteServicePort},
						MeshExternal: false,
						Resolution:   model.ClientSideLB,
					},
				})
			}
		}
	}

	return instances, nil
}

// GetProxyServiceInstances returns all service instances running on a particular proxy
// Cloud Foundry integration is currently ingress-only -- there is no sidecar support yet.
// So this function always returns an empty slice.
func (sd *ServiceDiscovery) GetProxyServiceInstances(proxy *model.Proxy) ([]*model.ServiceInstance, error) {
	return nil, nil
}

// ManagementPorts is not currently implemented for Cloud Foundry
func (sd *ServiceDiscovery) ManagementPorts(addr string) model.PortList {
	return nil
}

// all CF apps listen on the same port (for now)
func (sd *ServiceDiscovery) servicePort() *model.Port {
	return &model.Port{
		Port:     sd.ServicePort,
		Protocol: model.ProtocolHTTP,
		Name:     "http",
	}
}
