/*
 * Copyright 2018, CS Systemes d'Information, http://www.c-s.fr
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package handlers

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/CS-SI/SafeScale/broker/client"
	"github.com/CS-SI/SafeScale/broker/server/metadata"
	brokerutils "github.com/CS-SI/SafeScale/broker/utils"
	"github.com/CS-SI/SafeScale/iaas"
	"github.com/CS-SI/SafeScale/iaas/resources"
	"github.com/CS-SI/SafeScale/iaas/resources/enums/HostProperty"
	"github.com/CS-SI/SafeScale/iaas/resources/enums/HostState"
	"github.com/CS-SI/SafeScale/iaas/resources/enums/IPVersion"
	"github.com/CS-SI/SafeScale/iaas/resources/enums/NetworkProperty"
	propsv1 "github.com/CS-SI/SafeScale/iaas/resources/properties/v1"
	"github.com/CS-SI/SafeScale/system"
	"github.com/CS-SI/SafeScale/utils"
	"github.com/CS-SI/SafeScale/utils/retry"
)

//go:generate mockgen -destination=../mocks/mock_hostapi.go -package=mocks github.com/CS-SI/SafeScale/broker/server/handlers HostAPI

// TODO At service level, we need to log before returning, because it's the last chance to track the real issue in server side

// HostAPI defines API to manipulate hosts
type HostAPI interface {
	Create(name string, net string, cpu int, ram float32, disk int, os string, public bool, gpuNumber int, freq float32, force bool) (*resources.Host, error)
	List(all bool) ([]*resources.Host, error)
	ForceInspect(ref string) (*resources.Host, error)
	Inspect(ref string) (*resources.Host, error)
	Delete(ref string) error
	SSH(ref string) (*system.SSHConfig, error)
	Reboot(ref string) error
	Resize(name string, cpu int, ram float32, disk int, gpuNumber int, freq float32) (*resources.Host, error)
	Start(ref string) error
	Stop(ref string) error
}

// HostHandler host service
type HostHandler struct {
	service *iaas.Service
}

// NewHostHandler ...
func NewHostHandler(svc *iaas.Service) HostAPI {
	return &HostHandler{
		service: svc,
	}
}

// Start starts a host
func (handler *HostHandler) Start(ref string) error {
	log.Debugf(">>> broker.server.handlers.HostHandler::Start(%s)", ref)
	defer log.Debugf("<<< broker.server.handlers.HostHandler::Start(%s)", ref)

	mh, err := metadata.LoadHost(handler.service, ref)
	if err != nil {
		// TODO Introduce error level as parameter
		return infraErrf(err, "Error getting ssh config of host '%s': loading host metadata", ref)
	}
	if mh == nil {
		return infraErr(fmt.Errorf("host '%s' not found", ref))
	}
	id := mh.Get().ID
	err = handler.service.StartHost(id)
	if err != nil {
		return infraErr(err)
	}
	return infraErr(handler.service.WaitHostState(id, HostState.STARTED, brokerutils.GetTimeoutCtxHost()))
}

// Stop stops a host
func (handler *HostHandler) Stop(ref string) error {
	log.Debugf(">>> broker.server.handlers.HostHandler::Stop(%s)", ref)
	defer log.Debugf("<<< broker.server.handlers.HostHandler::Stop(%s)", ref)

	mh, err := metadata.LoadHost(handler.service, ref)
	if err != nil {
		// TODO Introduce error level as parameter
		return infraErrf(err, "Error getting ssh config of host '%s': loading host metadata", ref)
	}
	if mh == nil {
		return infraErr(fmt.Errorf("host '%s' not found", ref))
	}
	id := mh.Get().ID
	err = handler.service.StopHost(id)
	if err != nil {
		return infraErr(err)
	}
	return infraErr(handler.service.WaitHostState(id, HostState.STOPPED, brokerutils.GetTimeoutCtxHost()))
}

// Reboot reboots a host
func (handler *HostHandler) Reboot(ref string) error {
	log.Debugf(">>> broker.server.handlers.HostHandler::Reboot(%s)", ref)
	defer log.Debugf("<<< broker.server.handlers.HostHandler::Reboot(%s)", ref)

	mh, err := metadata.LoadHost(handler.service, ref)
	if err != nil {
		return infraErr(fmt.Errorf("failed to load metadata of host '%s': %v", ref, err))
	}
	if mh == nil {
		return infraErr(fmt.Errorf("host '%s' not found", ref))
	}
	id := mh.Get().ID
	err = handler.service.RebootHost(id)
	if err != nil {
		return infraErr(err)
	}
	err = retry.WhileUnsuccessfulDelay5Seconds(
		func() error {
			return handler.service.WaitHostState(id, HostState.STARTED, brokerutils.GetTimeoutCtxHost())
		},
		5*time.Minute,
	)
	if err != nil {
		return infraErrf(err, "timeout waiting host '%s' to be rebooted", ref)
	}
	return nil
}

// Resize ...
func (handler *HostHandler) Resize(ref string, cpu int, ram float32, disk int, gpuNumber int, freq float32) (*resources.Host, error) {
	log.Debugf(">>> broker.server.handlers.HostHandler::Resize(%s)", ref)
	defer log.Debugf("<<< broker.server.handlers.HostHandler::Resize(%s)", ref)

	mh, err := metadata.LoadHost(handler.service, ref)
	if err != nil {
		return nil, infraErrf(err, "failed to load host metadata")
	}
	if mh == nil {
		return nil, throwErrf("host '%s' not found", ref)
	}

	id := mh.Get().ID
	hostSizeRequest := resources.SizingRequirements{
		MinDiskSize: disk,
		MinRAMSize:  ram,
		MinCores:    cpu,
		MinFreq:     freq,
		MinGPU:      gpuNumber,
	}

	// TODO RESIZE 1st check new requirements vs old requirements
	host := mh.Get()
	host, err = handler.service.GetHost(host)
	if err != nil {
		return nil, infraErr(err)
	}

	if host.Properties.Lookup(HostProperty.SizingV1) {
		descent := false
		err = host.Properties.LockForRead(HostProperty.SizingV1).ThenUse(func(v interface{}) error {
			nhs := v.(*propsv1.HostSizing)
			descent = descent || (hostSizeRequest.MinCores < nhs.RequestedSize.Cores)
			descent = descent || (hostSizeRequest.MinRAMSize < nhs.RequestedSize.RAMSize)
			descent = descent || (hostSizeRequest.MinGPU < nhs.RequestedSize.GPUNumber)
			descent = descent || (hostSizeRequest.MinFreq < nhs.RequestedSize.CPUFreq)
			descent = descent || (hostSizeRequest.MinDiskSize < nhs.RequestedSize.DiskSize)
			return nil
		})
		if err != nil {
			return nil, infraErrf(err, "Unable to parse host metadata '%s", ref)
		}
		if descent {
			log.Warn("Asking for less resources..., ain't gonna happen :(")
		}
	}

	newHost, err := handler.service.ResizeHost(id, hostSizeRequest)

	if err != nil {
		return nil, infraErrf(err, "Error resizing host '%s'", ref)
	}
	if newHost == nil {
		return nil, throwErrf("Unknown error resizing host '%s'", ref)
	}

	return newHost, err
}

// Create creates a host
func (handler *HostHandler) Create(
	name string, net string, cpu int, ram float32, disk int, los string, public bool, gpuNumber int, freq float32, force bool,
) (*resources.Host, error) {

	log.Debugf(">>> broker.server.handlers.host.go:HostHandler::Create(%s)", name)
	defer log.Debugf("<<< broker.server.handlers.hostHostHandler::Create(%s)", name)

	host, err := handler.service.GetHostByName(name)
	if err != nil {
		if _, ok := err.(resources.ErrResourceNotFound); !ok {
			return nil, infraErrf(err, "failure creating host: failed to check if host resource name '%s' is already used: %v", name, err)
		}
	} else {
		return nil, logicErr(fmt.Errorf("failed to create host '%s': name is already used", name))
	}

	var networks []*resources.Network
	var gw *resources.Host
	if len(net) != 0 {
		networkHandler := NewNetworkHandler(handler.service)
		n, err := networkHandler.Inspect(net)
		if err != nil {
			if _, ok := err.(resources.ErrResourceNotFound); ok {
				return nil, infraErr(err)
			}
			return nil, infraErrf(err, "failed to get network resource data: '%s'", net)
		}
		if n == nil {
			return nil, logicErr(fmt.Errorf("failed to find network '%s'", net))
		}
		networks = append(networks, n)
		mgw, err := metadata.LoadHost(handler.service, n.GatewayID)
		if err != nil {
			return nil, infraErr(err)
		}
		if mgw == nil {
			return nil, logicErr(fmt.Errorf("failed to find gateway of network '%s'", net))
		}
		gw = mgw.Get()
	} else {
		net, err := handler.getOrCreateDefaultNetwork()
		if err != nil {
			return nil, infraErr(err)
		}
		networks = append(networks, net)
	}

	templates, err := handler.service.SelectTemplatesBySize(
		resources.SizingRequirements{
			MinCores:    cpu,
			MinRAMSize:  ram,
			MinDiskSize: disk,
			MinGPU:      gpuNumber,
			MinFreq:     freq,
		}, force)
	if err != nil {
		return nil, infraErrf(err, "failed to find template corresponding to requested resources")
	}
	var template resources.HostTemplate
	if len(templates) > 0 {
		template = templates[0]
		msg := fmt.Sprintf("Selected host template: '%s' (%d core%s", template.Name, template.Cores, utils.Plural(template.Cores))
		if template.CPUFreq > 0 {
			msg += fmt.Sprintf(" at %.01f GHz", template.CPUFreq)
		}
		msg += fmt.Sprintf(", %.01f GB RAM, %d GB disk", template.RAMSize, template.DiskSize)
		if template.GPUNumber > 0 {
			msg += fmt.Sprintf(", %d GPU%s", template.GPUNumber, utils.Plural(template.GPUNumber))
			if template.GPUType != "" {
				msg += fmt.Sprintf(" %s", template.GPUType)
			}
		}
		msg += ")"
		log.Infof(msg)
	} else {
		log.Errorf("failed to find template corresponding to requested resources")
		return nil, logicErrf(err, "failed to find template corresponding to requested resources")
	}

	var img *resources.Image
	err = retry.WhileUnsuccessfulDelay1Second(
		func() error {
			var innerErr error
			img, innerErr = handler.service.SearchImage(los)
			return innerErr
		},
		10*time.Second,
	)
	if err != nil {
		return nil, infraErrf(err, "failed to find image to use on compute resource")
	}

	hostRequest := resources.HostRequest{
		ImageID:        img.ID,
		ResourceName:   name,
		TemplateID:     template.ID,
		PublicIP:       public,
		Networks:       networks,
		DefaultGateway: gw,
	}

	host, err = handler.service.CreateHost(hostRequest)
	if err != nil {
		if _, ok := err.(resources.ErrResourceInvalidRequest); ok {
			return nil, infraErr(err)
		}
		return nil, infraErrf(err, "failed to create compute resource '%s'", hostRequest.ResourceName)
	}

	defer func() {
		if err != nil {
			derr := handler.service.DeleteHost(host.ID)
			if derr != nil {
				log.Errorf("Failed to delete host '%s': %v", host.Name, derr)
			}
		}
	}()

	// Updates property propsv1.HostSizing
	if host == nil {
		return nil, throwErrf("unexpected error creating host instance: host is nil !")
	}
	if host.Properties == nil {
		return nil, throwErrf("error populating host properties: host.Properties is nil !")
	}

	err = host.Properties.LockForWrite(HostProperty.SizingV1).ThenUse(func(v interface{}) error {
		hostSizingV1 := v.(*propsv1.HostSizing)
		hostSizingV1.Template = hostRequest.TemplateID
		hostSizingV1.RequestedSize = &propsv1.HostSize{
			Cores:     cpu,
			RAMSize:   ram,
			DiskSize:  disk,
			GPUNumber: gpuNumber,
			CPUFreq:   freq,
		}
		return nil
	})
	if err != nil {
		return nil, infraErr(err)
	}
	// TODO OPP Unsafe

	// Sets host extension DescriptionV1
	creator := ""
	hostname, _ := os.Hostname()
	if curUser, err := user.Current(); err == nil {
		creator := curUser.Username
		if hostname != "" {
			creator += "@" + hostname
		}
		if curUser.Name != "" {
			creator += " (" + curUser.Name + ")"
		}
	} else {
		creator = "unknown@" + hostname
	}
	err = host.Properties.LockForWrite(HostProperty.DescriptionV1).ThenUse(func(v interface{}) error {
		hostDescriptionV1 := v.(*propsv1.HostDescription)
		hostDescriptionV1.Created = time.Now()
		hostDescriptionV1.Creator = creator
		return nil
	})
	if err != nil {
		return nil, infraErr(err)
	}

	// Updates host property propsv1.HostNetwork
	var (
		defaultNetworkID string
		gatewayID        string
	)
	err = host.Properties.LockForWrite(HostProperty.NetworkV1).ThenUse(func(v interface{}) error {
		hostNetworkV1 := v.(*propsv1.HostNetwork)
		defaultNetworkID = hostNetworkV1.DefaultNetworkID // set earlier by handler.service.CreateHost()
		if !public {
			if len(networks) > 0 {
				mgw, err := metadata.LoadGateway(handler.service, defaultNetworkID)
				if err == nil {
					gatewayID = mgw.Get().ID
				}
			}
		}
		hostNetworkV1.DefaultGatewayID = gatewayID

		if net != "" {
			mn, err := metadata.LoadNetwork(handler.service, net)
			if err != nil {
				return err
			}
			network := mn.Get()
			hostNetworkV1.NetworksByID[network.ID] = network.Name
			hostNetworkV1.NetworksByName[network.Name] = network.ID
		}

		return nil
	})
	if err != nil {
		return nil, infraErr(err)
	}

	// Updates host metadata
	mh := metadata.NewHost(handler.service)
	err = mh.Carry(host).Write()
	if err != nil {
		return nil, infraErrf(err, "Metadata creation failed")
	}
	log.Infof("Compute resource created: '%s'", host.Name)

	// Starting from here, remove metadata if exiting with error
	defer func() {
		if err != nil {
			derr := mh.Delete()
			if derr != nil {
				log.Errorf("failed to remove host metadata after host creation failure")
			}
		}
	}()

	// A host claimed ready by a Cloud provider is not necessarily ready
	// to be used until ssh service is up and running. So we wait for it before
	// claiming host is created
	log.Infof("Waiting start of SSH service on remote host '%s' ...", host.Name)
	sshHandler := NewSSHHandler(handler.service)
	sshCfg, err := sshHandler.GetConfig(host.ID)
	if err != nil {
		return nil, infraErr(err)
	}
	sshDefaultTimeout := int(brokerutils.GetTimeoutCtxHost().Minutes())
	if sshDefaultTimeoutCandidate := os.Getenv("SSH_TIMEOUT"); sshDefaultTimeoutCandidate != "" {
		num, err := strconv.Atoi(sshDefaultTimeoutCandidate)
		if err == nil {
			log.Debugf("Using custom timeout of %d minutes", num)
			sshDefaultTimeout = num
		}
	}
	// TODO configurable timeout here
	err = sshCfg.WaitServerReady(time.Duration(sshDefaultTimeout) * time.Minute)
	if err != nil {
		return nil, infraErr(err)
	}
	if client.IsTimeout(err) {
		return nil, infraErrf(err, "Timeout creating a host")
	}

	log.Infof("SSH service started on host '%s'.", host.Name)

	// Updates host link with networks
	for _, i := range networks {
		err = i.Properties.LockForWrite(NetworkProperty.HostsV1).ThenUse(func(v interface{}) error {
			networkHostsV1 := v.(*propsv1.NetworkHosts)
			networkHostsV1.ByName[host.Name] = host.ID
			networkHostsV1.ByID[host.ID] = host.Name
			return nil
		})
		if err != nil {
			log.Errorf(err.Error())
			continue
		}
		_, err = metadata.SaveNetwork(handler.service, i)
		if err != nil {
			log.Errorf(err.Error())
		}
	}
	// Don't want to remove host if network metadata fails...
	err = nil

	return host, nil
}

// getOrCreateDefaultNetwork gets network resources.SingleHostNetworkName or create it if necessary
// We don't want metadata on this network, so we use directly provider api instead of services
func (handler *HostHandler) getOrCreateDefaultNetwork() (*resources.Network, error) {
	network, err := handler.service.GetNetworkByName(resources.SingleHostNetworkName)
	if err != nil {
		switch err.(type) {
		case resources.ErrResourceNotFound:
		default:
			return nil, infraErr(err)
		}
	}
	if network != nil {
		return network, nil
	}

	request := resources.NetworkRequest{
		Name:      resources.SingleHostNetworkName,
		IPVersion: IPVersion.IPv4,
		CIDR:      "10.0.0.0/8",
	}

	mnet, err := handler.service.CreateNetwork(request)
	return mnet, infraErr(err)
}

// List returns the host list
func (handler *HostHandler) List(all bool) ([]*resources.Host, error) {
	log.Debugf("<<< broker.server.handlers.HostHandler::List(%v)", all)
	defer log.Debugf(">>> broker.server.handlers.HostHandler::List(%v)", all)

	if all {
		return handler.service.ListHosts()
	}

	var hosts []*resources.Host
	m := metadata.NewHost(handler.service)
	err := m.Browse(func(host *resources.Host) error {
		hosts = append(hosts, host)
		return nil
	})
	if err != nil {
		return hosts, infraErrf(err, "Error listing monitored hosts: browse")
	}
	return hosts, nil
}

// ForceInspect ...
// If not found, return (nil, err)
func (handler *HostHandler) ForceInspect(ref string) (*resources.Host, error) {
	log.Debugf(">>> broker.server.handlers.HostHandler::ForceInspect(%s)", ref)
	defer log.Debugf("<<< broker.server.handlers.HostHandler::ForceInspect(%s)", ref)

	host, err := handler.Inspect(ref)
	if err != nil {
		return nil, throwErr(err)
	}

	return host, nil
}

// Inspect returns the host identified by ref, ref can be the name or the id
// If not found, returns (nil, nil)
func (handler *HostHandler) Inspect(ref string) (*resources.Host, error) {
	log.Debugf(">>> broker.server.handlers.HostHandler::Inspect(%s)", ref)
	defer log.Debugf("<<< broker.server.handlers.HostHandler::Inspect(%s)", ref)

	mh, err := metadata.LoadHost(handler.service, ref)
	if err != nil {
		if _, ok := err.(utils.ErrNotFound); ok {
			return nil, resources.ResourceNotFoundError("host", ref)
		}
		return nil, throwErr(err)
	}

	host := mh.Get()
	host, err = handler.service.GetHost(host)
	if err != nil {
		return nil, infraErr(err)
	}
	return host, nil
}

// Delete deletes host referenced by ref
func (handler *HostHandler) Delete(ref string) error {
	log.Debugf(">>> broker.server.handlers.HostHandler::Delete(%s)", ref)
	defer log.Debugf("<<< broker.server.handlers.HostHandler::Delete(%s)", ref)

	mh, err := metadata.LoadHost(handler.service, ref)
	if err != nil {
		if _, ok := err.(utils.ErrNotFound); ok {
			return resources.ResourceNotFoundError("host", ref)
		}
		return infraErrf(err, "can't delete host '%s'", ref)
	}

	host := mh.Get()
	// Don't remove a host having shares that are currently remotely mounted
	var shares map[string]*propsv1.HostShare
	err = host.Properties.LockForRead(HostProperty.SharesV1).ThenUse(func(v interface{}) error {
		shares = v.(*propsv1.HostShares).ByID
		for _, share := range shares {
			count := len(share.ClientsByID)
			if count > 0 {
				count = len(shares)
				return logicErr(fmt.Errorf("can't delete host, exports %d share%s where at least one is used", count, utils.Plural(count)))
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Don't remove a host with volumes attached
	err = host.Properties.LockForRead(HostProperty.VolumesV1).ThenUse(func(v interface{}) error {
		nAttached := len(v.(*propsv1.HostVolumes).VolumesByID)
		if nAttached > 0 {
			return logicErr(fmt.Errorf("host has %d volume%s attached", nAttached, utils.Plural(nAttached)))
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Don't remove a host that is a gateway
	err = host.Properties.LockForRead(HostProperty.NetworkV1).ThenUse(func(v interface{}) error {
		if v.(*propsv1.HostNetwork).IsGateway {
			return logicErr(fmt.Errorf("can't delete host, it's a gateway that can only be deleted through its network"))
		}
		return nil
	})
	if err != nil {
		return err
	}

	// If host mounted shares, unmounts them before anything else
	shareHandler := NewShareHandler(handler.service)
	var mounts []*propsv1.HostShare
	err = host.Properties.LockForRead(HostProperty.MountsV1).ThenUse(func(v interface{}) error {
		hostMountsV1 := v.(*propsv1.HostMounts)
		for _, i := range hostMountsV1.RemoteMountsByPath {
			// Gets share data
			_, share, _, err := shareHandler.Inspect(i.ShareID)
			if err != nil {
				return infraErr(err)
			}
			if share == nil {
				return resources.ResourceNotFoundError("share", i.ShareID)
			}
			mounts = append(mounts, share)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Unmounts tier shares mounted on host (done outside the previous host.Properties.Reading() section, because
	// Unmount() have to lock for write, and won't succeed while host.Properties.Reading() is running,
	// leading to a deadlock)
	for _, share := range mounts {
		err = shareHandler.Unmount(share.Name, host.Name)
		if err != nil {
			return infraErr(err)
		}
	}

	// if host exports shares, delete them
	for _, share := range shares {
		err = shareHandler.Delete(share.Name)
		if err != nil {
			return throwErr(err)
		}
	}

	// Update networks property prosv1.NetworkHosts to remove the reference to the host
	netHandler := NewNetworkHandler(handler.service)
	err = host.Properties.LockForRead(HostProperty.NetworkV1).ThenUse(func(v interface{}) error {
		hostNetworkV1 := v.(*propsv1.HostNetwork)
		for k := range hostNetworkV1.NetworksByID {
			network, err := netHandler.Inspect(k)
			if err != nil {
				log.Errorf(err.Error())
				continue
			}
			err = network.Properties.LockForWrite(NetworkProperty.HostsV1).ThenUse(func(v interface{}) error {
				networkHostsV1 := v.(*propsv1.NetworkHosts)
				delete(networkHostsV1.ByID, host.ID)
				delete(networkHostsV1.ByName, host.Name)
				return nil
			})
			if err != nil {
				log.Errorf(err.Error())
			}
			_, err = metadata.SaveNetwork(handler.service, network)
			if err != nil {
				log.Errorf(err.Error())
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Conditions are met, delete host
	err = handler.service.DeleteHost(host.ID)
	if err != nil {
		return infraErrf(err, "can't delete host")
	}

	// Finally, delete metadata of host
	trydelete := mh.Delete()
	return infraErr(trydelete)
}

// SSH returns ssh parameters to access the host referenced by ref
func (handler *HostHandler) SSH(ref string) (*system.SSHConfig, error) {
	log.Debugf(">>> broker.server.handlers.HostHandler::SSH(%s)", ref)
	defer log.Debugf("<<< broker.server.handlers.HostHandler::SSH(%s)", ref)

	// host, err := svc.Inspect(ref)
	// if err != nil {
	// 	return nil, logicErrf(err, fmt.Sprintf("can't access ssh parameters of host '%s': failed to query host", ref), "")
	// }

	sshHandler := NewSSHHandler(handler.service)
	// sshConfig, err := sshHandler.GetConfig(host.ID)
	sshConfig, err := sshHandler.GetConfig(ref)
	if err != nil {
		return nil, logicErr(err)
	}
	return sshConfig, nil
}
