/*
 * Copyright 2018-2020, CS Systemes d'Information, http://www.c-s.fr
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

package huaweicloud

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/pengux/check"
	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"

	"github.com/gophercloud/gophercloud"
	nics "github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/attachinterfaces"
	exbfv "github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/bootfromvolume"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/floatingips"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/flavors"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/openstack/networking/v2/ports"
	"github.com/gophercloud/gophercloud/pagination"

	"github.com/CS-SI/SafeScale/lib/server/iaas/stacks/openstack"
	"github.com/CS-SI/SafeScale/lib/server/iaas/userdata"
	"github.com/CS-SI/SafeScale/lib/server/resources/abstract"
	"github.com/CS-SI/SafeScale/lib/server/resources/enums/hoststate"
	"github.com/CS-SI/SafeScale/lib/server/resources/enums/ipversion"
	"github.com/CS-SI/SafeScale/lib/server/resources/operations/converters"
	"github.com/CS-SI/SafeScale/lib/utils"
	"github.com/CS-SI/SafeScale/lib/utils/concurrency"
	"github.com/CS-SI/SafeScale/lib/utils/retry"
	"github.com/CS-SI/SafeScale/lib/utils/scerr"
	"github.com/CS-SI/SafeScale/lib/utils/strprocess"
	"github.com/CS-SI/SafeScale/lib/utils/temporal"
)

type blockDevice struct {
	// SourceType must be one of: "volume", "snapshot", "image", or "blank".
	SourceType exbfv.SourceType `json:"source_type" required:"true"`

	// UUID is the unique identifier for the existing volume, snapshot, or
	// image (see above).
	UUID string `json:"uuid,omitempty"`

	// BootIndex is the boot index. It defaults to 0.
	BootIndex string `json:"boot_index,omitempty"`

	// DeleteOnTermination specifies whether or not to delete the attached volume
	// when the server is deleted. Defaults to `false`.
	DeleteOnTermination bool `json:"delete_on_termination"`

	// DestinationType is the type that gets created. Possible values are "volume"
	// and "local".
	DestinationType exbfv.DestinationType `json:"destination_type,omitempty"`

	// GuestFormat specifies the format of the block device.
	GuestFormat string `json:"guest_format,omitempty"`

	// VolumeSize is the size of the volume to create (in gigabytes). This can be
	// omitted for existing volumes.
	VolumeSize int `json:"volume_size,omitempty"`

	// Type of volume
	VolumeType string `json:"volume_type,omitempty"`
}

// CreateOptsExt is a structure that extends the server `CreateOpts` structure
// by allowing for a block device mapping.
type bootdiskCreateOptsExt struct {
	servers.CreateOptsBuilder
	BlockDevice []blockDevice `json:"block_device_mapping_v2,omitempty"`
}

// ToServerCreateMap adds the block device mapping option to the base server
// creation options.
func (opts bootdiskCreateOptsExt) ToServerCreateMap() (map[string]interface{}, error) {
	base, err := opts.CreateOptsBuilder.ToServerCreateMap()
	if err != nil {
		return nil, err
	}

	if len(opts.BlockDevice) == 0 {
		err := gophercloud.ErrMissingInput{}
		err.Argument = "bootfromvolume.CreateOptsExt.BlockDevice"
		return nil, err
	}

	serverMap, ok := base["server"].(map[string]interface{})
	if !ok {
		return nil, scerr.InvalidParameterError("base['server']", "is not a map[string]")
	}

	blkDevices := make([]map[string]interface{}, len(opts.BlockDevice))

	for i, bd := range opts.BlockDevice {
		b, err := gophercloud.BuildRequestBody(bd, "")
		if err != nil {
			return nil, err
		}
		blkDevices[i] = b
	}
	serverMap["block_device_mapping_v2"] = blkDevices

	return base, nil
}

type serverCreateOpts struct {
	// Name is the name to assign to the newly launched server.
	Name string `json:"name" required:"true"`

	// ImageRef [optional; required if ImageName is not provided] is the ID or
	// full URL to the image that contains the server's OS and initial state.
	// Also optional if using the boot-from-volume extension.
	ImageRef string `json:"imageRef,omitempty"`

	// ImageName [optional; required if ImageRef is not provided] is the name of
	// the image that contains the server's OS and initial state.
	// Also optional if using the boot-from-volume extension.
	ImageName string `json:"-,omitempty"`

	// FlavorRef [optional; required if FlavorName is not provided] is the ID or
	// full URL to the flavor that describes the server's specs.
	FlavorRef string `json:"flavorRef"`

	// FlavorName [optional; required if FlavorRef is not provided] is the name of
	// the flavor that describes the server's specs.
	FlavorName string `json:"-"`

	// SecurityGroups lists the names of the security groups to which this server
	// should belong.
	SecurityGroups []string `json:"-"`

	// UserData contains configuration information or scripts to use upon launch.
	// Create will base64-encode it for you, if it isn't already.
	UserData []byte `json:"-"`

	// AvailabilityZone in which to launch the server.
	AvailabilityZone string `json:"availability_zone,omitempty"`

	// Networks dictates how this server will be attached to available networks.
	// By default, the server will be attached to all isolated networks for the
	// tenant.
	Networks []servers.Network `json:"-"`

	// Metadata contains key-value pairs (up to 255 bytes each) to attach to the
	// server.
	Metadata map[string]string `json:"metadata,omitempty"`

	// Personality includes files to inject into the server at launch.
	// Create will base64-encode file contents for you.
	Personality servers.Personality `json:"personality,omitempty"`

	// ConfigDrive enables metadata injection through a configuration drive.
	ConfigDrive *bool `json:"config_drive,omitempty"`

	// AdminPass sets the root user password. If not set, a randomly-generated
	// password will be created and returned in the response.
	AdminPass string `json:"adminPass,omitempty"`

	// AccessIPv4 specifies an IPv4 address for the instance.
	AccessIPv4 string `json:"accessIPv4,omitempty"`

	// AccessIPv6 pecifies an IPv6 address for the instance.
	AccessIPv6 string `json:"accessIPv6,omitempty"`

	// ServiceClient will allow calls to be made to retrieve an image or
	// flavor ID by name.
	ServiceClient *gophercloud.ServiceClient `json:"-"`
}

// ToServerCreateMap assembles a request body based on the contents of a
// CreateOpts.
func (opts serverCreateOpts) ToServerCreateMap() (map[string]interface{}, error) {
	sc := opts.ServiceClient
	opts.ServiceClient = nil
	b, err := gophercloud.BuildRequestBody(opts, "")
	if err != nil {
		return nil, err
	}

	if opts.UserData != nil {
		var userData string
		if _, err := base64.StdEncoding.DecodeString(string(opts.UserData)); err != nil {
			userData = base64.StdEncoding.EncodeToString(opts.UserData)
		} else {
			userData = string(opts.UserData)
		}
		// logrus.Debugf("Base64 encoded userdata size = %d bytes", len(userData))
		b["user_data"] = &userData
	}

	if len(opts.SecurityGroups) > 0 {
		securityGroups := make([]map[string]interface{}, len(opts.SecurityGroups))
		for i, groupName := range opts.SecurityGroups {
			securityGroups[i] = map[string]interface{}{"name": groupName}
		}
		b["security_groups"] = securityGroups
	}

	if len(opts.Networks) > 0 {
		networks := make([]map[string]interface{}, len(opts.Networks))
		for i, network := range opts.Networks {
			networks[i] = make(map[string]interface{})
			if network.UUID != "" {
				networks[i]["uuid"] = network.UUID
			}
			if network.Port != "" {
				networks[i]["port"] = network.Port
			}
			if network.FixedIP != "" {
				networks[i]["fixed_ip"] = network.FixedIP
			}
		}
		b["networks"] = networks
	}

	// If FlavorRef isn't provided, use FlavorName to ascertain the flavor ID.
	if opts.FlavorRef == "" {
		if opts.FlavorName == "" {
			err := servers.ErrNeitherFlavorIDNorFlavorNameProvided{}
			err.Argument = "FlavorRef/FlavorName"
			return nil, err
		}
		if sc == nil {
			err := servers.ErrNoClientProvidedForIDByName{}
			err.Argument = "ServiceClient"
			return nil, err
		}
		flavorID, err := flavors.IDFromName(sc, opts.FlavorName)
		if err != nil {
			return nil, err
		}
		b["flavorRef"] = flavorID
	}

	return map[string]interface{}{"server": b}, nil
}

// CreateHost creates a new host
// On success returns an instance of abstract.Host, and a string containing the script to execute to finalize host installation
func (s *Stack) CreateHost(request abstract.HostRequest) (host *abstract.HostFull, userData *userdata.Content, err error) {
	if s == nil {
		return nil, nil, scerr.InvalidInstanceError()
	}

	tracer := concurrency.NewTracer(nil, true, "(%s)", request.ResourceName).WithStopwatch().Entering()
	defer tracer.OnExitTrace()()
	defer scerr.OnExitLogError(tracer.TraceMessage(""), &err)()
	defer scerr.OnPanic(&err)()

	userData = userdata.NewContent()

	//msgFail := "failed to create Host resource: %s"
	msgSuccess := fmt.Sprintf("Host resource '%s' created successfully", request.ResourceName)

	if request.DefaultGateway == nil && !request.PublicIP {
		return nil, userData, abstract.ResourceInvalidRequestError("host creation", "cannot create a host without network and without public access (would be unreachable)")
	}

	// Validating name of the host
	if ok, err := validatehostName(request); !ok {
		return nil, userData, scerr.InvalidRequestError("name '%s' is invalid for a FlexibleEngine Host: %s", request.ResourceName, openstack.ProviderErrorToString(err))
	}

	// The Default Network is the first of the provided list, by convention
	defaultNetwork := request.Networks[0]
	defaultNetworkID := defaultNetwork.ID
	defaultGateway := request.DefaultGateway
	isGateway := defaultGateway == nil && defaultNetwork.Name != abstract.SingleHostNetworkName
	defaultGatewayID := ""
	defaultGatewayPrivateIP := ""
	if defaultGateway != nil {
		// // FIXME: defaultGatewayPrivateIP and defaultGatewayID must come by request
		// err := defaultGateway.Inspect(func(_ data.Clonable, props *serialize.JSONProperties) error {
		// 	return props.Inspect(hostproperty.NetworkV1, func(clonable data.Clonable) error {
		// 		hostNetworkV1 := clonable.(*propertiesv1.HostNetwork)
		// 		defaultGatewayPrivateIP = hostNetworkV1.IPv4Addresses[defaultNetworkID]
		defaultGatewayID = defaultGateway.ID
		// 		return nil
		// 	})
		// })
		// if err != nil {
		// 	return nil, nil, nil, nil, userData, err
		// }
	}

	var nets []servers.Network
	// Add private networks
	for _, n := range request.Networks {
		nets = append(nets, servers.Network{
			UUID: n.ID,
		})
	}

	// If no key pair is supplied create one
	if request.KeyPair == nil {
		id, err := uuid.NewV4()
		if err != nil {
			return nil, userData, scerr.Wrap(err, "error creating UID")
		}

		name := fmt.Sprintf("%s_%s", request.ResourceName, id)
		request.KeyPair, err = s.CreateKeyPair(name)
		if err != nil {
			msg := fmt.Sprintf("failed to create host key pair: %+v", err)
			logrus.Debugf(strprocess.Capitalize(msg))
		}
	}
	if request.Password == "" {
		password, err := utils.GeneratePassword(16)
		if err != nil {
			return nil, userData, scerr.Wrap(err, "failed to generate password")
		}
		request.Password = password
	}

	// --- prepares data structures for Provider usage ---

	// Constructs userdata content
	err = userData.Prepare(s.cfgOpts, request, defaultNetwork.CIDR, "")
	if err != nil {
		err = scerr.Wrap(err, "failed to prepare user data content")
		logrus.Debugf(strprocess.Capitalize(err.Error()))
		return nil, userData, err
	}

	template, err := s.GetTemplate(request.TemplateID)
	if err != nil {
		return nil, userData, scerr.NewError("failed to get image: %s", openstack.ProviderErrorToString(err))
	}

	// Determines appropriate disk size
	if request.DiskSize > template.DiskSize {
		template.DiskSize = request.DiskSize
	} else if template.DiskSize == 0 {
		if template.Cores < 16 { // nolint
			template.DiskSize = 100
		} else if template.Cores < 32 {
			template.DiskSize = 200
		} else {
			template.DiskSize = 400
		}
	}

	// Select usable availability zone
	az, err := s.SelectedAvailabilityZone()
	if err != nil {
		return nil, userData, err
	}

	// Defines boot disk
	bootdiskOpts := blockDevice{
		SourceType:          exbfv.SourceImage,
		DestinationType:     exbfv.DestinationVolume,
		BootIndex:           "0",
		DeleteOnTermination: true,
		UUID:                request.ImageID,
		VolumeType:          "SSD",
		VolumeSize:          template.DiskSize,
	}
	// Defines server
	userDataPhase1, err := userData.Generate("phase1")
	if err != nil {
		return nil, userData, err
	}
	srvOpts := serverCreateOpts{
		Name:             request.ResourceName,
		SecurityGroups:   []string{s.SecurityGroup.Name},
		Networks:         nets,
		FlavorRef:        request.TemplateID,
		UserData:         userDataPhase1,
		AvailabilityZone: az,
	}
	// Defines host "Extension bootfromvolume" options
	bdOpts := bootdiskCreateOptsExt{
		CreateOptsBuilder: srvOpts,
		BlockDevice:       []blockDevice{bootdiskOpts},
	}
	b, err := bdOpts.ToServerCreateMap()
	if err != nil {
		return nil, userData, scerr.NewError("failed to build query to create host '%s': %s", request.ResourceName, openstack.ProviderErrorToString(err))
	}

	// --- Initializes abstract.HostFull ---

	host = abstract.NewHostFull()
	host.Core.Name = request.ResourceName
	host.Core.PrivateKey = request.KeyPair.PrivateKey // Add PrivateKey to host definition
	host.Core.Password = request.Password
	host.Network.IsGateway = isGateway
	host.Network.DefaultNetworkID = defaultNetworkID
	host.Network.DefaultGatewayID = defaultGatewayID
	host.Network.DefaultGatewayPrivateIP = defaultGatewayPrivateIP
	// Note: from there, no idea what was the RequestedSize; caller will have to complement this information
	host.Sizing = converters.HostTemplateToHostEffectiveSizing(template)

	// --- query provider for host creation ---

	// Retry creation until success, for 10 minutes
	var (
		httpResp *http.Response
		r        servers.CreateResult
	)

	retryErr := retry.WhileUnsuccessfulDelay5Seconds(
		func() error {
			httpResp, r.Err = s.Stack.ComputeClient.Post(s.Stack.ComputeClient.ServiceURL("servers"), b, &r.Body, &gophercloud.RequestOpts{
				OkCodes: []int{200, 202},
			})
			server, ierr := r.Extract()
			if ierr != nil {
				if server != nil {
					servers.Delete(s.Stack.ComputeClient, server.ID)
				}
				var codeStr string
				if httpResp != nil {
					codeStr = fmt.Sprintf(" (HTTP return code: %d)", httpResp.StatusCode)
				}
				return scerr.NewError("query to create host '%s' failed: %s%s", request.ResourceName, openstack.ProviderErrorToString(ierr), codeStr)
			}

			creationZone, zoneErr := s.GetAvailabilityZoneOfServer(server.ID)
			if zoneErr != nil {
				logrus.Tracef("Host successfully created but can't confirm AZ: %s", zoneErr)
			} else {
				logrus.Tracef("Host successfully created in requested AZ '%s'", creationZone)
				if creationZone != srvOpts.AvailabilityZone {
					if srvOpts.AvailabilityZone != "" {
						logrus.Warnf("Host created in the WRONG availability zone: requested '%s' and got instead '%s'", srvOpts.AvailabilityZone, creationZone)
					}
				}
			}

			host.Core.ID = server.ID

			defer func() {
				if ierr != nil {
					servers.Delete(s.ComputeClient, server.ID)
				}
			}()

			// Wait that host is ready, not just that the build is started
			host.Core, err = s.WaitHostReady(host, temporal.GetHostTimeout())
			if err != nil {
				switch err.(type) {
				case scerr.ErrNotAvailable:
					return fmt.Errorf("host '%s' is in ERROR state", request.ResourceName)
				default:
					return fmt.Errorf("timeout waiting host '%s' ready: %s", request.ResourceName, openstack.ProviderErrorToString(err))
				}
			}
			return nil
		},
		temporal.GetLongOperationTimeout(),
	)
	if retryErr != nil {
		err = retryErr
		return nil, userData, err
	}
	if host == nil {
		return nil, userData, scerr.NewError("unexpected problem creating host")
	}

	newHost := host

	// Starting from here, delete host if exiting with error
	defer func() {
		if err != nil {
			derr := s.DeleteHost(newHost.Core.ID)
			if derr != nil {
				switch derr.(type) {
				case scerr.ErrNotFound:
					logrus.Errorf("Cleaning up on failure, failed to delete host '%s', resource not found: '%v'", newHost.Core.Name, derr)
				case scerr.ErrTimeout:
					logrus.Errorf("Cleaning up on failure, failed to delete host '%s', timeout: '%v'", newHost.Core.Name, derr)
				default:
					logrus.Errorf("Cleaning up on failure, failed to delete host '%s': '%v'", newHost.Core.Name, derr)
				}
				err = scerr.AddConsequence(err, derr)
			}
		}
	}()

	if request.PublicIP {
		var fip *FloatingIP
		fip, err = s.attachFloatingIP(host.Core)
		if err != nil {
			spew.Dump(err)
			return nil, userData, scerr.NewError("error attaching public IP for host '%s': %s", request.ResourceName, openstack.ProviderErrorToString(err))
		}
		if fip == nil {
			return nil, userData, scerr.NewError("error attaching public IP for host: unknown error")
		}

		// Starting from here, delete Floating IP if exiting with error
		defer func() {
			if err != nil {
				derr := s.DeleteFloatingIP(fip.ID)
				if derr != nil {
					logrus.Errorf("Error deleting Floating IP: %v", derr)
					err = scerr.AddConsequence(err, derr)
				}
			}
		}()

		if ipversion.IPv4.Is(fip.PublicIPAddress) {
			host.Network.PublicIPv4 = fip.PublicIPAddress
		} else if ipversion.IPv6.Is(fip.PublicIPAddress) {
			host.Network.PublicIPv6 = fip.PublicIPAddress
		}
		userData.PublicIP = fip.PublicIPAddress

		if defaultGateway == nil && defaultNetwork.Name != abstract.SingleHostNetworkName {
			err = s.enableHostRouterMode(host)
			if err != nil {
				return nil, userData, scerr.NewError("error enabling gateway mode of host '%s': %s", request.ResourceName, openstack.ProviderErrorToString(err))
			}
		}
	}

	logrus.Infoln(msgSuccess)
	return host, userData, nil
}

// validatehostName validates the name of an host based on known FlexibleEngine requirements
func validatehostName(req abstract.HostRequest) (bool, error) {
	s := check.Struct{
		"ResourceName": check.Composite{
			check.NonEmpty{},
			check.Regex{Constraint: `^[a-zA-Z0-9_-]+$`},
			check.MaxChar{Constraint: 64},
		},
	}

	e := s.Validate(req)
	if e.HasErrors() {
		errorList, _ := e.GetErrorsByKey("ResourceName")
		var errs []error
		for _, msg := range errorList {
			errs = append(errs, msg)
		}
		return false, scerr.ErrListError(errs)
	}
	return true, nil
}

// InspectHost updates the data inside host with the data from provider
func (s *Stack) InspectHost(hostParam interface{}) (host *abstract.HostFull, err error) {
	if s == nil {
		return nil, scerr.InvalidInstanceError()
	}

	var server *servers.Server
	host = abstract.NewHostFull()
	switch hostParam := hostParam.(type) {
	case string:
		if hostParam == "" {
			return nil, scerr.InvalidParameterError("hostParam", "cannot be an empty string")
		}
		host.Core.ID = hostParam
	case *abstract.HostCore:
		if hostParam == nil {
			return nil, scerr.InvalidParameterError("hostParam", "cannot be nil")
		}
		host.Core = hostParam
	default:
		return nil, scerr.InvalidParameterError("hostParam", "must be a string or a *abstract.Host")
	}
	hostRef := host.Core.ID

	server, err = s.queryServer(hostRef)
	if err != nil {
		return nil, err
	}
	if server == nil {
		return nil, abstract.ResourceNotFoundError("host", hostRef)
	}

	if host, err = s.complementHost(host.Core, server); err != nil {
		return nil, err
	}

	if !host.OK() {
		logrus.Warnf("[TRACE] Unexpected host status: %s", spew.Sdump(host))
	}

	return host, err
}

func (s *Stack) queryServer(id string) (server *servers.Server, err error) {
	server, err = s.waitHostState(id, hoststate.STARTED, 2*temporal.GetBigDelay())
	if err != nil {
		return nil, err
	}

	if server == nil {
		return nil, abstract.ResourceNotFoundError("host", id)
	}

	return server, nil
}

// complementHost complements Host data with content of server parameter
func (s *Stack) complementHost(host *abstract.HostCore, server *servers.Server) (completedHost *abstract.HostFull, err error) {
	defer scerr.OnPanic(&err)()

	networks, addresses, ipv4, ipv6, err := s.collectAddresses(host)
	if err != nil {
		return nil, err
	}

	// Updates intrinsic data of host if needed
	if host.ID == "" {
		host.ID = server.ID
	}
	if host.Name == "" {
		host.Name = server.Name
	}
	host.LastState = toHostState(server.Status)
	if host.LastState != hoststate.STARTED {
		logrus.Warnf("[TRACE] Unexpected host's last state: %v", host.LastState)
	}

	completedHost.Core = host
	completedHost.Description.Created = server.Created
	completedHost.Description.Updated = server.Updated

	if completedHost.Network.PublicIPv4 == "" {
		completedHost.Network.PublicIPv4 = ipv4
	}
	if completedHost.Network.PublicIPv6 == "" {
		completedHost.Network.PublicIPv6 = ipv6
	}
	if len(completedHost.Network.NetworksByID) > 0 {
		ipv4Addresses := map[string]string{}
		ipv6Addresses := map[string]string{}
		for netid, netname := range completedHost.Network.NetworksByID {
			if ip, ok := addresses[ipversion.IPv4][netid]; ok {
				ipv4Addresses[netid] = ip
			} else if ip, ok := addresses[ipversion.IPv4][netname]; ok {
				ipv4Addresses[netid] = ip
			} else {
				ipv4Addresses[netid] = ""
			}

			if ip, ok := addresses[ipversion.IPv6][netid]; ok {
				ipv6Addresses[netid] = ip
			} else if ip, ok := addresses[ipversion.IPv6][netname]; ok {
				ipv6Addresses[netid] = ip
			} else {
				ipv6Addresses[netid] = ""
			}
		}
		completedHost.Network.IPv4Addresses = ipv4Addresses
		completedHost.Network.IPv6Addresses = ipv6Addresses
	} else {
		networksByID := map[string]string{}
		ipv4Addresses := map[string]string{}
		ipv6Addresses := map[string]string{}
		for _, netid := range networks {
			networksByID[netid] = ""

			if ip, ok := addresses[ipversion.IPv4][netid]; ok {
				ipv4Addresses[netid] = ip
			} else {
				ipv4Addresses[netid] = ""
			}

			if ip, ok := addresses[ipversion.IPv6][netid]; ok {
				ipv6Addresses[netid] = ip
			} else {
				ipv6Addresses[netid] = ""
			}
		}
		completedHost.Network.NetworksByID = networksByID
		// IPvxAddresses are here indexed by names... At least we have them...
		completedHost.Network.IPv4Addresses = ipv4Addresses
		completedHost.Network.IPv6Addresses = ipv6Addresses
	}

	// Updates network name and relationships if needed
	var errors []error
	for netid, netname := range completedHost.Network.NetworksByID {
		if netname == "" {
			network, err := s.GetNetwork(netid)
			if err != nil {
				logrus.Errorf("failed to get network '%s'", netid)
				errors = append(errors, err)
				continue
			}
			completedHost.Network.NetworksByID[netid] = network.Name
			completedHost.Network.NetworksByName[network.Name] = netid
		}
	}
	if len(errors) > 0 {
		return nil, scerr.ErrListError(errors)
	}

	return completedHost, nil
}

// collectAddresses converts adresses returned by the OpenStack driver
// Returns string slice containing the name of the networks, string map of IP addresses
// (indexed on network name), public ipv4 and ipv6 (if they exists)
func (s *Stack) collectAddresses(host *abstract.HostCore) ([]string, map[ipversion.Enum]map[string]string, string, string, error) {
	var (
		networks      []string
		addrs         = map[ipversion.Enum]map[string]string{}
		AcccessIPv4   string
		AcccessIPv6   string
		allInterfaces []nics.Interface
	)

	pager := s.listInterfaces(host.ID)
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		list, err := nics.ExtractInterfaces(page)
		if err != nil {
			return false, err
		}
		allInterfaces = append(allInterfaces, list...)
		return true, nil
	})
	if err != nil {
		return networks, addrs, "", "", err
	}

	addrs[ipversion.IPv4] = map[string]string{}
	addrs[ipversion.IPv6] = map[string]string{}

	for _, item := range allInterfaces {
		networks = append(networks, item.NetID)
		for _, address := range item.FixedIPs {
			fixedIP := address.IPAddress
			ipv4 := net.ParseIP(fixedIP).To4() != nil
			if item.NetID == s.cfgOpts.ProviderNetwork {
				if ipv4 {
					AcccessIPv4 = fixedIP
				} else {
					AcccessIPv6 = fixedIP
				}
			} else {
				if ipv4 {
					addrs[ipversion.IPv4][item.NetID] = fixedIP
				} else {
					addrs[ipversion.IPv6][item.NetID] = fixedIP
				}
			}
		}
	}
	return networks, addrs, AcccessIPv4, AcccessIPv6, nil
}

// ListHosts lists available hosts
func (s *Stack) ListHosts(details bool) (abstract.HostList, error) {
	if s == nil {
		return nil, scerr.InvalidInstanceError()
	}

	pager := servers.List(s.Stack.ComputeClient, servers.ListOpts{})
	var hostList abstract.HostList
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		list, err := servers.ExtractServers(page)
		if err != nil {
			return false, err
		}

		for _, srv := range list {
			h := abstract.NewHostCore()
			h.ID = srv.ID
			var ah *abstract.HostFull
			if details {
				ah, err = s.complementHost(h, &srv)
				if err != nil {
					return false, err
				}
			} else {
				ah = abstract.NewHostFull()
				ah.Core = h
			}
			hostList = append(hostList, ah)
		}
		return true, nil
	})
	if len(hostList) == 0 && err != nil {
		return nil, scerr.NewError("error listing hosts: %s", openstack.ProviderErrorToString(err))
	}
	return hostList, nil
}

// DeleteHost deletes the host identified by id
func (s *Stack) DeleteHost(id string) error {
	if s == nil {
		return scerr.InvalidInstanceError()
	}

	_, err := s.InspectHost(id)
	if err != nil {
		return err
	}

	if s.cfgOpts.UseFloatingIP {
		fip, err := s.getFloatingIPOfHost(id)
		if err == nil {
			if fip != nil {
				err = floatingips.DisassociateInstance(s.Stack.ComputeClient, id, floatingips.DisassociateOpts{
					FloatingIP: fip.IP,
				}).ExtractErr()
				if err != nil {
					return scerr.NewError("error deleting host %s : %s", id, openstack.ProviderErrorToString(err))
				}
				err = floatingips.Delete(s.Stack.ComputeClient, fip.ID).ExtractErr()
				if err != nil {
					return scerr.NewError("error deleting host %s : %s", id, openstack.ProviderErrorToString(err))
				}
			}
		}
	}

	// Try to remove host for 3 minutes
	outerRetryErr := retry.WhileUnsuccessful(
		func() error {
			resourcePresent := true
			// 1st, send delete host order
			err = servers.Delete(s.Stack.ComputeClient, id).ExtractErr()
			if err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault404:
					// Resource not found, consider deletion succeeded (if the entry doesn't exist at all,
					// metadata deletion will return an error)
					return nil
				default:
					return scerr.NewError("failed to submit host '%s' deletion: %s", id, openstack.ProviderErrorToString(err))
				}
			}
			// 2nd, check host status every 5 seconds until check failed.
			// If check succeeds but state is Error, retry the deletion.
			// If check fails and error isn't 'resource not found', retry
			var host *servers.Server
			innerRetryErr := retry.WhileUnsuccessfulDelay5Seconds(
				func() error {
					host, err = servers.Get(s.Stack.ComputeClient, id).Extract()
					if err == nil {
						if toHostState(host.Status) == hoststate.ERROR {
							return nil
						}
						return scerr.NewError("host '%s' state is '%s'", host.Name, host.Status)
					}
					// FIXME: capture more error types
					switch err.(type) { // nolint
					case gophercloud.ErrDefault404:
						resourcePresent = false
						return nil
					}
					return err
				},
				temporal.GetContextTimeout(),
			)
			if innerRetryErr != nil {
				if _, ok := innerRetryErr.(retry.ErrTimeout); ok {
					// retry deletion...
					return scerr.Wrap(abstract.ResourceTimeoutError("host", id, temporal.GetContextTimeout()),
						"host '%s' not deleted after %v", id, temporal.GetContextTimeout())
				}
				return innerRetryErr
			}
			if !resourcePresent {
				return nil
			}
			return scerr.NewError("host '%s' in state 'ERROR', retrying to delete", id)
		},
		0,
		temporal.GetHostCleanupTimeout(),
	)
	if outerRetryErr != nil {
		logrus.Errorf("failed to remove host '%s': %s", id, outerRetryErr.Error())
		return err
	}
	return nil
}

// getFloatingIP returns the floating IP associated with the host identified by hostID
// By convention only one floating IP is allocated to an host
func (s *Stack) getFloatingIPOfHost(hostID string) (*floatingips.FloatingIP, error) {
	pager := floatingips.List(s.Stack.ComputeClient)
	var fips []floatingips.FloatingIP
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		list, err := floatingips.ExtractFloatingIPs(page)
		if err != nil {
			return false, err
		}

		for _, fip := range list {
			if fip.InstanceID == hostID {
				fips = append(fips, fip)
			}
		}
		return true, nil
	})
	if len(fips) == 0 {
		if err != nil {
			return nil, scerr.NotFoundError("no floating IP found for host '%s': %s", hostID, openstack.ProviderErrorToString(err))
		}
		return nil, scerr.NotFoundError("no floating IP found for host '%s'", hostID)

	}
	if len(fips) > 1 {
		return nil, scerr.InconsistentError("configuration error, more than one Floating IP associated to host '%s'", hostID)
	}
	return &fips[0], nil
}

// attachFloatingIP creates a Floating IP and attaches it to an host
func (s *Stack) attachFloatingIP(host *abstract.HostCore) (*FloatingIP, error) {
	fip, err := s.CreateFloatingIP()
	if err != nil {
		return nil, scerr.NewError("failed to attach Floating IP on host '%s': %s", host.Name, openstack.ProviderErrorToString(err))
	}

	err = s.AssociateFloatingIP(host, fip.ID)
	if err != nil {
		derr := s.DeleteFloatingIP(fip.ID)
		if derr != nil {
			logrus.Warnf("Error deleting floating ip: %v", derr)
		}
		return nil, scerr.NewError("failed to attach Floating IP to host '%s': %s", host.Name, openstack.ProviderErrorToString(err))
	}
	return fip, nil
}

// EnableHostRouterMode enables the host to act as a router/gateway.
func (s *Stack) enableHostRouterMode(host *abstract.HostFull) error {
	var (
		portID *string
		err    error
	)

	// Sometimes, getOpenstackPortID doesn't find network interface, so let's retry in case it's a bad timing issue
	retryErr := retry.WhileUnsuccessfulDelay5SecondsTimeout(
		func() error {
			portID, err = s.getOpenstackPortID(host)
			if err != nil {
				return scerr.NewError("%s", openstack.ProviderErrorToString(err))
			}
			if portID == nil {
				return scerr.NewError("failed to find OpenStack port")
			}
			return nil
		},
		temporal.GetBigDelay(),
	)
	if retryErr != nil {
		return scerr.Wrap(retryErr, "failed to enable Router Mode on host '%s'", host.Core.Name)
	}

	pairs := []ports.AddressPair{
		{
			IPAddress: "1.1.1.1/0",
		},
	}
	opts := ports.UpdateOpts{AllowedAddressPairs: &pairs}
	_, err = ports.Update(s.Stack.NetworkClient, *portID, opts).Extract()
	if err != nil {
		return scerr.NewError("failed to enable Router Mode on host '%s': %s", host.Core.Name, openstack.ProviderErrorToString(err))
	}
	return nil
}

// DisableHostRouterMode disables the host to act as a router/gateway.
func (s *Stack) disableHostRouterMode(host *abstract.HostFull) error {
	portID, err := s.getOpenstackPortID(host)
	if err != nil {
		return scerr.NewError("failed to disable Router Mode on host '%s': %s", host.Core.Name, openstack.ProviderErrorToString(err))
	}
	if portID == nil {
		return scerr.NewError("failed to disable Router Mode on host '%s': failed to find OpenStack port", host.Core.Name)
	}

	opts := ports.UpdateOpts{AllowedAddressPairs: nil}
	_, err = ports.Update(s.Stack.NetworkClient, *portID, opts).Extract()
	if err != nil {
		return scerr.NewError("failed to disable Router Mode on host '%s': %s", host.Core.Name, openstack.ProviderErrorToString(err))
	}
	return nil
}

// listInterfaces returns a pager of the interfaces attached to host identified by 'serverID'
func (s *Stack) listInterfaces(hostID string) pagination.Pager {
	url := s.Stack.ComputeClient.ServiceURL("servers", hostID, "os-interface")
	return pagination.NewPager(s.Stack.ComputeClient, url, func(r pagination.PageResult) pagination.Page {
		return nics.InterfacePage{SinglePageBase: pagination.SinglePageBase(r)}
	})
}

// getOpenstackPortID returns the port ID corresponding to the first private IP address of the host
// returns nil,nil if not found
func (s *Stack) getOpenstackPortID(host *abstract.HostFull) (*string, error) {
	ip := host.Network.IPv4Addresses[host.Network.DefaultNetworkID]
	found := false
	nic := nics.Interface{}
	pager := s.listInterfaces(host.Core.ID)
	err := pager.EachPage(func(page pagination.Page) (bool, error) {
		list, err := nics.ExtractInterfaces(page)
		if err != nil {
			return false, err
		}
		for _, i := range list {
			for _, iip := range i.FixedIPs {
				if iip.IPAddress == ip {
					found = true
					nic = i
					return false, nil
				}
			}
		}
		return true, nil
	})
	if err != nil {
		return nil, scerr.NewError("error browsing Openstack Interfaces of host '%s': %s", host.Core.Name, openstack.ProviderErrorToString(err))
	}
	if found {
		return &nic.PortID, nil
	}
	return nil, abstract.ResourceNotFoundError("Port ID corresponding to host", host.Core.Name)
}

// toHostSizing converts flavor attributes returned by OpenStack driver into abstract.HostEffectiveSizing
func (s *Stack) toHostSizing(flavor map[string]interface{}) *abstract.HostEffectiveSizing {
	if i, ok := flavor["id"]; ok {
		fid, ok := i.(string)
		if !ok {
			return nil
		}
		tpl, err := s.GetTemplate(fid)
		if err != nil {
			return nil
		}
		return converters.HostTemplateToHostEffectiveSizing(tpl)
	}
	hostSizing := &abstract.HostEffectiveSizing{}
	if _, ok := flavor["vcpus"]; ok {
		hostSizing.Cores, _ = flavor["vcpus"].(int)
		hostSizing.DiskSize, _ = flavor["disk"].(int)
		hostSizing.RAMSize, _ = flavor["ram"].(float32)
		hostSizing.RAMSize /= 1000.0
	}
	return hostSizing
}

// toHostState converts host status returned by FlexibleEngine driver into HostState enum
func toHostState(status string) hoststate.Enum {
	switch status {
	case "BUILD", "build", "BUILDING", "building":
		return hoststate.STARTING
	case "ACTIVE", "active":
		return hoststate.STARTED
	case "RESCUED", "rescued":
		return hoststate.STOPPING
	case "STOPPED", "stopped", "SHUTOFF", "shutoff":
		return hoststate.STOPPED
	default:
		return hoststate.ERROR
	}
}

// WaitHostReady waits an host achieve ready state
// hostParam can be an ID of host, or an instance of *abstract.Host; any other type will return an utils.ErrInvalidParameter.
func (s *Stack) WaitHostReady(hostParam interface{}, timeout time.Duration) (*abstract.HostCore, error) {
	if s == nil {
		return nil, scerr.InvalidInstanceError()
	}

	var host *abstract.HostCore
	switch hostParam := hostParam.(type) {
	case string:
		if hostParam == "" {
			return nil, scerr.InvalidParameterError("hostParam", "cannot be empty string")
		}
		host = abstract.NewHostCore()
		host.ID = hostParam
	case *abstract.HostCore:
		if hostParam == nil {
			return nil, scerr.InvalidParameterError("hostParam", "canot be nil")
		}
		host = hostParam
	default:
		return nil, scerr.InvalidParameterError("hostParam", "must be a non-empty string or a *abstract.HostCore")
	}
	hostRef := host.Name
	if hostRef == "" {
		hostRef = host.ID
	}

	defer concurrency.NewTracer(nil, true, "(%s)", hostRef).WithStopwatch().Entering().OnExitTrace()()

	retryErr := retry.WhileUnsuccessful(
		func() error {
			hostTmp, innerErr := s.InspectHost(hostRef)
			if innerErr != nil {
				return innerErr
			}
			if hostTmp.Core.LastState == hoststate.ERROR {
				return retry.StopRetryError(nil, "host '%s' in error state", hostRef)
			}
			host = hostTmp.Core
			if host.LastState != hoststate.STARTED {
				return scerr.NotAvailableError("not in ready state (current state: %s)", host.LastState.String())
			}
			return nil
		},
		temporal.GetDefaultDelay(),
		timeout,
	)
	if retryErr != nil {
		switch retryErr.(type) {
		case *retry.ErrStopRetry:
			return nil, abstract.ResourceNotAvailableError("host", "hostRef")
		case *retry.ErrTimeout:
			return host, abstract.ResourceTimeoutError("host", hostRef, timeout)
		}
		return host, retryErr
	}
	return host, nil
}

// waitHostState waits an host achieve ready state
// hostParam can be an ID of host, or an instance of *abstract.HostCore; any other type will return an utils.ErrInvalidParameter
func (s *Stack) waitHostState(hostParam interface{}, state hoststate.Enum, timeout time.Duration) (server *servers.Server, err error) {
	if s == nil {
		return nil, scerr.InvalidInstanceError()
	}

	var hostCore *abstract.HostCore

	switch hostParam := hostParam.(type) {
	case string:
		hostCore = abstract.NewHostCore()
		hostCore.ID = hostParam
	case *abstract.HostCore:
		hostCore = hostParam
	}
	if hostCore == nil {
		return nil, scerr.InvalidParameterError("hostParam", "must be a not-empty string or a *abstract.Host!")
	}

	defer concurrency.NewTracer(nil, true, "(%s)", hostCore.ID).WithStopwatch().Entering().OnExitTrace()()

	retryErr := retry.WhileUnsuccessful(
		func() error {
			server, err = servers.Get(s.ComputeClient, hostCore.ID).Extract()
			if err != nil {
				switch err.(type) {
				case gophercloud.ErrDefault404:
					// If error is "resource not found", we want to return GopherCloud error as-is to be able
					// to behave differently in this special case. To do so, stop the retry
					return retry.StopRetryError(abstract.ResourceNotFoundError("host", hostCore.ID), "")
				case gophercloud.ErrDefault408:
					// Server timeout
					return err
				case gophercloud.ErrDefault409:
					// specific handling for error 409
					return scerr.AbortedError("", fmt.Errorf("error getting host '%s': %s", hostCore.ID, err))
				case gophercloud.ErrDefault429:
					// rate limiting defined by provider, retry
					return err
				case gophercloud.ErrDefault503:
					// service unavailable, retry
					return err
				case gophercloud.ErrDefault500:
					// When the response is "Internal Server Error", retry
					return err
				}

				errorCode, failed := openstack.GetUnexpectedGophercloudErrorCode(err)
				if failed == nil {
					switch errorCode {
					case 408:
						return err
					case 429:
						return err
					case 500:
						return err
					case 503:
						return err
					default:
						return retry.StopRetryError(fmt.Errorf("error getting host '%s': code: %d, reason: %s", hostCore.ID, errorCode, err), "")
					}
				}

				if openstack.IsServiceUnavailableError(err) {
					return err
				}

				// Any other error stops the retry
				return retry.StopRetryError(fmt.Errorf("error getting host '%s': %s", hostCore.ID, err), "")
			}

			if server == nil {
				return scerr.NewError(nil, nil, "error getting host, nil response from gophercloud")
			}

			lastState := toHostState(server.Status)
			// If state matches, we consider this a success no matter what
			if lastState == state {
				return nil
			}

			if lastState == hoststate.ERROR {
				return retry.StopRetryError(abstract.ResourceNotAvailableError("host", hostCore.ID), "")
			}

			if lastState != hoststate.STARTING && lastState != hoststate.STOPPING {
				return retry.StopRetryError(fmt.Errorf("host status of '%s' is in state '%s', and that's not a transition state", hostCore.ID, server.Status), "")
			}

			return fmt.Errorf("server not ready yet")
		},
		temporal.GetMinDelay(),
		timeout,
	)
	if retryErr != nil {
		if timeoutErr, ok := retryErr.(retry.ErrTimeout); ok {
			return nil, scerr.TimeoutError(timeoutErr.Cause(), timeout, "timeout waiting to get host '%s' information after %v", hostCore.ID, timeout)
		}

		if aborted, ok := retryErr.(retry.ErrStopRetry); ok {
			return nil, aborted.Cause()
		}

		return nil, retryErr
	}

	return server, nil
}
