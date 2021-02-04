/*
 * Copyright 2018-2020, CS Systemes d'Information, http://csgroup.eu
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
	"fmt"

	"github.com/CS-SI/SafeScale/lib/utils/fail"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"github.com/gophercloud/gophercloud/pagination"

	"github.com/CS-SI/SafeScale/lib/server/iaas/abstract"
	"github.com/CS-SI/SafeScale/lib/server/iaas/stacks/openstack"
)

// ListOpts to define parameter of list
type ListOpts struct {
	Marker string `json:"marker,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type bandwidthCreateOpts struct {
	Name       string `json:"name"`
	Size       int    `json:"size"`
	ShareType  string `json:"share_type"`
	ChargeMode string `json:"charge_mode,omitempty"`
}

func (opts bandwidthCreateOpts) toBandwidthCreateMap() (map[string]interface{}, fail.Error) {
	return gophercloud.BuildRequestBody(opts, "bandwidth")
}

type ipCreateOpts struct {
	Type      string `json:"type"`
	IPAddress string `json:"ip_address,omitempty"`
}

func (opts ipCreateOpts) toFloatingIPCreateMap() (map[string]interface{}, fail.Error) {
	return gophercloud.BuildRequestBody(opts, "publicip")
}

// FloatingIP represents a FlexibleEngine Floating IP
type FloatingIP struct {
	ID              string `json:"id"`
	Status          string `json:"status"`
	Type            string `json:"type"`
	PublicIPAddress string `json:"public_ip_address"`
	TenantID        string `json:"tenant_id"`
	CreateTime      string `json:"create_time"`
	BandwidthSize   int    `json:"bandwidth_size"`
}

type floatingIPPage struct {
	pagination.LinkedPageBase
}

// NextPageURL is invoked when a paginated collection of floating IPs has
// reached the end of a page and the pager seeks to traverse over a new one.
// In order to do this, it needs to construct the next page's URL.
func (r floatingIPPage) NextPageURL() (string, fail.Error) {
	var s struct {
		Links []gophercloud.Link `json:"floatingips_links"`
	}
	err := r.ExtractInto(&s)
	if err != nil {
		return "", err
	}
	return gophercloud.ExtractNextURL(s.Links)
}

// IsEmpty checks whether a FloatingIPPage struct is empty.
func (r floatingIPPage) IsEmpty() (bool, fail.Error) {
	is, err := extractFloatingIPs(r)
	return len(is) == 0, err
}

// extractFloatingIPs accepts a Page struct, specifically a FloatingIPPage
// struct, and extracts the elements into a slice of FloatingIP structs. In
// other words, a generic collection is mapped into a relevant slice.
func extractFloatingIPs(r pagination.Page) ([]FloatingIP, fail.Error) {
	var s struct {
		FloatingIPs []FloatingIP `json:"floatingips"`
	}
	err := (r.(floatingIPPage)).ExtractInto(&s)
	return s.FloatingIPs, err
}

type commonResult struct {
	gophercloud.Result
}

// Extract will extract a FloatingIP resource from a result.
func (r commonResult) Extract() (*FloatingIP, fail.Error) {
	var s struct {
		FloatingIP *FloatingIP `json:"publicip"`
	}
	err := r.ExtractInto(&s)
	return s.FloatingIP, err
}

// CreateResult represents the result of a create operation. Call its Extract
// method to interpret it as a FloatingIP.
type createResult struct {
	commonResult
}

// GetResult represents the result of a get operation. Call its Extract
// method to interpret it as a FloatingIP.
type getResult struct {
	commonResult
}

type deleteResult struct {
	gophercloud.ErrResult
}

// ListFloatingIPs lists all the floating IP currently requested for the VPC
func (s *Stack) ListFloatingIPs() pagination.Pager {
	url := s.Stack.NetworkClient.Endpoint + "v1/" + s.authOpts.ProjectID + "/publicips"
	return pagination.NewPager(
		s.Stack.NetworkClient, url, func(r pagination.PageResult) pagination.Page {
			return floatingIPPage{pagination.LinkedPageBase{PageResult: r}}
		},
	)
}

// GetFloatingIP returns FloatingIP instance corresponding to ID 'id'
func (s *Stack) GetFloatingIP(id string) (*FloatingIP, fail.Error) {
	r := getResult{}
	url := s.Stack.NetworkClient.Endpoint + "v1/" + s.authOpts.ProjectID + "/publicips/" + id
	opts := gophercloud.RequestOpts{
		JSONResponse: &r.Body,
		OkCodes:      []int{200, 201},
	}
	_, err := s.Stack.Driver.Request("GET", url, &opts)
	r.Err = err
	fip, err := r.Extract()
	if err != nil {
		return nil, fail.Errorf(
			fmt.Sprintf(
				"failed to get information for Floating IP id '%s': %s", id, openstack.ProviderErrorToString(err),
			), err,
		)
	}
	return fip, nil
}

// FindFloatingIPByIP returns FloatingIP instance associated with 'ipAddress'
func (s *Stack) FindFloatingIPByIP(ipAddress string) (*FloatingIP, fail.Error) {
	pager := s.ListFloatingIPs()
	found := false
	fip := FloatingIP{}
	err := pager.EachPage(
		func(page pagination.Page) (bool, fail.Error) {
			list, err := extractFloatingIPs(page)
			if err != nil {
				return false, err
			}
			for _, i := range list {
				if i.PublicIPAddress == ipAddress {
					found = true
					fip = i
					return false, nil
				}
			}
			return true, nil
		},
	)
	if err != nil {
		return nil, fail.Errorf(
			fmt.Sprintf(
				"failed to browser Floating IPs: %s", openstack.ProviderErrorToString(err),
			), err,
		)
	}
	if found {
		return &fip, nil
	}
	return nil, abstract.ResourceNotFoundError("Floating IP", ipAddress)
}

// CreateFloatingIP creates a floating IP
func (s *Stack) CreateFloatingIP() (*FloatingIP, fail.Error) {
	ipOpts := ipCreateOpts{
		Type: "5_bgp",
	}
	bi, err := ipOpts.toFloatingIPCreateMap()
	if err != nil {
		return nil, fail.Errorf(
			fmt.Sprintf(
				"failed to build request to create FloatingIP: %s", openstack.ProviderErrorToString(err),
			), err,
		)
	}
	bandwidthOpts := bandwidthCreateOpts{
		Name:      "bandwidth-" + s.vpc.Name,
		Size:      1000,
		ShareType: "PER",
	}
	bb, err := bandwidthOpts.toBandwidthCreateMap()
	if err != nil {
		return nil, fail.Errorf(
			fmt.Sprintf(
				"failed to build request to create FloatingIP: %s", openstack.ProviderErrorToString(err),
			), err,
		)
	}
	// Merger bi in bb
	for k, v := range bi {
		bb[k] = v
	}

	r := createResult{}
	url := s.Stack.NetworkClient.Endpoint + "v1/" + s.authOpts.ProjectID + "/publicips"
	opts := gophercloud.RequestOpts{
		JSONBody:     bb,
		JSONResponse: &r.Body,
		OkCodes:      []int{200, 201},
	}
	_, err = s.Stack.Driver.Request("POST", url, &opts)
	if err != nil {
		return nil, fail.Errorf(
			fmt.Sprintf(
				"failed to request Floating IP creation: %s", openstack.ProviderErrorToString(err),
			), err,
		)
	}
	fip, err := r.Extract()
	if err != nil {
		return nil, fail.Errorf(fmt.Sprintf("failed to create Floating IP: %s", err), err)
	}
	return fip, nil
}

// DeleteFloatingIP deletes a floating IP
func (s *Stack) DeleteFloatingIP(id string) error {
	r := deleteResult{}
	url := s.Stack.NetworkClient.Endpoint + "v1/" + s.authOpts.ProjectID + "/publicips/" + id
	opts := gophercloud.RequestOpts{
		JSONResponse: &r.Body,
		OkCodes:      []int{200, 201},
	}
	_, r.Err = s.Stack.Driver.Request("DELETE", url, &opts)
	err := r.ExtractErr()

	if err != nil {
		return fail.Wrap(err, "error deleting floating ip")
	}

	return nil
}

// AssociateFloatingIP to host
func (s *Stack) AssociateFloatingIP(host *abstract.Host, id string) error {
	fip, err := s.GetFloatingIP(id)
	if err != nil {
		return fail.Errorf(
			fmt.Sprintf(
				"failed to associate Floating IP id '%s' to host '%s': %s", id, host.Name,
				openstack.ProviderErrorToString(err),
			), err,
		)
	}

	b := map[string]interface{}{
		"addFloatingIp": map[string]string{
			"address": fip.PublicIPAddress,
		},
	}

	r := servers.ActionResult{}
	_, r.Err = s.Stack.ComputeClient.Post(s.Stack.ComputeClient.ServiceURL("servers", host.ID, "action"), b, nil, nil)
	err = r.ExtractErr()
	if err != nil {
		return fail.Errorf(
			fmt.Sprintf(
				"failed to associate Floating IP id '%s' to host '%s': %s", id, host.Name,
				openstack.ProviderErrorToString(err),
			), err,
		)
	}
	return nil
}

// DissociateFloatingIP from host
func (s *Stack) DissociateFloatingIP(host *abstract.Host, id string) error {
	fip, err := s.GetFloatingIP(id)
	if err != nil {
		return fail.Errorf(
			fmt.Sprintf(
				"failed to associate Floating IP id '%s' to host '%s': %s", id, host.Name,
				openstack.ProviderErrorToString(err),
			), err,
		)
	}

	b := map[string]interface{}{
		"removeFloatingIp": map[string]string{
			"address": fip.PublicIPAddress,
		},
	}

	r := servers.ActionResult{}
	_, r.Err = s.Stack.ComputeClient.Post(s.Stack.ComputeClient.ServiceURL("servers", host.ID, "action"), b, nil, nil)
	err = r.ExtractErr()
	if err != nil {
		return fail.Errorf(
			fmt.Sprintf(
				"failed to associate Floating IP id '%s' to host '%s': %s", id, host.Name,
				openstack.ProviderErrorToString(err),
			), err,
		)
	}
	return nil
}
