/*
 * Copyright 2018-2021, CS Systemes d'Information, http://csgroup.eu
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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	scribble "github.com/nanobox-io/golang-scribble"
	"github.com/sirupsen/logrus"

	"github.com/CS-SI/SafeScale/lib/protocol"
	"github.com/CS-SI/SafeScale/lib/server"
	"github.com/CS-SI/SafeScale/lib/server/resources"
	"github.com/CS-SI/SafeScale/lib/server/resources/abstract"
	"github.com/CS-SI/SafeScale/lib/server/resources/enums/ipversion"
	hostfactory "github.com/CS-SI/SafeScale/lib/server/resources/factories/host"
	networkfactory "github.com/CS-SI/SafeScale/lib/server/resources/factories/network"
	subnetfactory "github.com/CS-SI/SafeScale/lib/server/resources/factories/subnet"
	"github.com/CS-SI/SafeScale/lib/utils"
	"github.com/CS-SI/SafeScale/lib/utils/cli/enums/outputs"
	"github.com/CS-SI/SafeScale/lib/utils/data"
	"github.com/CS-SI/SafeScale/lib/utils/debug"
	"github.com/CS-SI/SafeScale/lib/utils/debug/tracing"
	"github.com/CS-SI/SafeScale/lib/utils/fail"
	"github.com/CS-SI/SafeScale/lib/utils/serialize"
	"github.com/CS-SI/SafeScale/lib/utils/temporal"
)

//go:generate mockgen -destination=../mocks/mock_imageapi.go -package=mocks github.com/CS-SI/SafeScale/lib/server/handlers TenantHandler

// PriceInfo stores price information
type PriceInfo struct {
	Currency      string  `json:"currency"`                 // contains the currency of the price info
	DurationLabel string  `json:"duration_label,omitempty"` // contains a label for the duration "Per Hour" for example
	Duration      uint    `json:"duration"`                 // number of seconds of the duration
	Price         float64 `json:"price"`                    // price in the given currency for the duration
}

// CPUInfo stores CPU properties
type CPUInfo struct {
	TenantName     string      `json:"tenant_name,omitempty"`
	TemplateID     string      `json:"template_id,omitempty"`
	TemplateName   string      `json:"template_name,omitempty"`
	ImageID        string      `json:"image_id,omitempty"`
	ImageName      string      `json:"image_name,omitempty"`
	LastUpdated    string      `json:"last_updated,omitempty"`
	NumberOfCPU    int         `json:"number_of_cpu,omitempty"`
	NumberOfCore   int         `json:"number_of_core,omitempty"`
	NumberOfSocket int         `json:"number_of_socket,omitempty"`
	CPUFrequency   float64     `json:"cpu_frequency_Ghz,omitempty"`
	CPUArch        string      `json:"cpu_arch,omitempty"`
	Hypervisor     string      `json:"hypervisor,omitempty"`
	CPUModel       string      `json:"cpu_model,omitempty"`
	RAMSize        float64     `json:"ram_size_Gb,omitempty"`
	RAMFreq        float64     `json:"ram_freq,omitempty"`
	GPU            int         `json:"gpu,omitempty"`
	GPUModel       string      `json:"gpu_model,omitempty"`
	DiskSize       int64       `json:"disk_size_Gb,omitempty"`
	MainDiskType   string      `json:"main_disk_type"`
	MainDiskSpeed  float64     `json:"main_disk_speed_MBps"`
	SampleNetSpeed float64     `json:"sample_net_speed_KBps"`
	EphDiskSize    int64       `json:"eph_disk_size_Gb"`
	PricePerSecond float64     `json:"price_in_dollars_second"` // DEPRECATED, use field Prices
	PricePerHour   float64     `json:"price_in_dollars_hour"`   // DEPRECATED, use field Prices
	Prices         []PriceInfo `json:"prices,omitempty"`
}

// StoredCPUInfo ...
type StoredCPUInfo struct {
	ID string `bow:"key"`
	CPUInfo
}

const (
	scanNetworkName   string  = "safescale-scan-network"
	scanNetworkCIDR   string  = "192.168.20.0/24"
	scanSubnetName    string  = "safescale-scan-subnet"
	scanSubnetCIDR    string  = "192.168.20.0/26"
	defaultScanImage  string  = "Ubuntu 18.04"
	scannedHostPrefix string  = "scanhost-"
	maxParallelScans  float64 = 4.0
)

const cmdNumberOfCPU string = "lscpu | grep 'CPU(s):' | grep -v 'NUMA' | tr -d '[:space:]' | cut -d: -f2"
const cmdNumberOfCorePerSocket string = "lscpu | grep 'Core(s) per socket' | tr -d '[:space:]' | cut -d: -f2"
const cmdNumberOfSocket string = "lscpu | grep 'Socket(s)' | tr -d '[:space:]' | cut -d: -f2"
const cmdArch string = "lscpu | grep 'Architecture' | tr -d '[:space:]' | cut -d: -f2"
const cmdHypervisor string = "lscpu | grep 'Hypervisor' | tr -d '[:space:]' | cut -d: -f2"

const cmdCPUFreq string = "lscpu | grep 'CPU MHz' | tr -d '[:space:]' | cut -d: -f2"
const cmdCPUModelName string = "lscpu | grep 'Model name' | cut -d: -f2 | sed -e 's/^[[:space:]]*//'"
const cmdTotalRAM string = "cat /proc/meminfo | grep MemTotal | cut -d: -f2 | sed -e 's/^[[:space:]]*//' | cut -d' ' -f1"
const cmdRAMFreq string = "sudo dmidecode -t memory | grep Speed | head -1 | cut -d' ' -f2"

// FIXME: some disks are vda (instead of sda)
const cmdGPU string = "lspci | egrep -i 'VGA|3D' | grep -i nvidia | cut -d: -f3 | sed 's/.*controller://g' | tr '\n' '%'"
const cmdDiskSize string = "lsblk -b --output SIZE -n -d /dev/sda"
const cmdEphemeralDiskSize string = "lsblk -o name,type,mountpoint | grep disk | awk {'print $1'} | grep -v sda | xargs -i'{}' lsblk -b --output SIZE -n -d /dev/'{}'"
const cmdRotational string = "cat /sys/block/sda/queue/rotational"
const cmdDiskSpeed string = "sudo hdparm -t --direct /dev/sda | grep MB | awk '{print $11}'"
const cmdNetSpeed string = "URL=\"http://www.google.com\";curl -L --w \"$URL\nDNS %{time_namelookup}s conn %{time_connect}s time %{time_total}s\nSpeed %{speed_download}bps Size %{size_download}bytes\n\" -o/dev/null -s $URL | grep bps | awk '{ print $2}' | cut -d '.' -f 1"

var cmd = fmt.Sprintf("export LANG=C;echo $(%s)î$(%s)î$(%s)î$(%s)î$(%s)î$(%s)î$(%s)î$(%s)î$(%s)î$(%s)î$(%s)î$(%s)î$(%s)î$(%s)î$(%s)",
	cmdNumberOfCPU,
	cmdNumberOfCorePerSocket,
	cmdNumberOfSocket,
	cmdCPUFreq,
	cmdArch,
	cmdHypervisor,
	cmdCPUModelName,
	cmdTotalRAM,
	cmdRAMFreq,
	cmdGPU,
	cmdDiskSize,
	cmdEphemeralDiskSize,
	cmdDiskSpeed,
	cmdRotational,
	cmdNetSpeed,
)

// TODO At service level, we need to log before returning, because it's the last chance to track the real issue in server side

// TenantHandler defines API to manipulate tenants
type TenantHandler interface {
	Scan(string, bool) (_ *protocol.ScanResultList, xerr fail.Error)
	Inspect(string) (_ *protocol.TenantInspectResponse, xerr fail.Error)
}

// tenantHandler service
type tenantHandler struct {
	job              server.Job
	abstractSubnet   *abstract.Subnet
	scannedHostImage *abstract.Image
}

// NewTenantHandler creates a scanner service
func NewTenantHandler(job server.Job) TenantHandler {
	return &tenantHandler{job: job}
}

// Scan scans the tenant and updates the database
func (handler *tenantHandler) Scan(tenantName string, isDryRun bool) (_ *protocol.ScanResultList, xerr fail.Error) {
	if handler == nil {
		return nil, fail.InvalidInstanceError()
	}
	if handler.job == nil {
		return nil, fail.InvalidInstanceContentError("handler.job", "cannot be nil")
	}
	if tenantName == "" {
		return nil, fail.InvalidParameterError("tenant name", "cannot be empty string")
	}

	tracer := debug.NewTracer(handler.job.GetTask(), tracing.ShouldTrace("handlers.tenant")).WithStopwatch().Entering()
	defer tracer.Exiting()
	defer fail.OnExitLogError(&xerr, tracer.TraceMessage())

	svc := handler.job.GetService()
	task := handler.job.GetTask()

	isScannable, err := handler.checkScannable()
	if err != nil {
		return nil, err
	}
	if !isScannable {
		return nil, fail.ForbiddenError("tenant is not scannable")
	}

	if isDryRun {
		return handler.dryRun()
	}

	if xerr = handler.dumpImages(); xerr != nil {
		return nil, xerr
	}

	if xerr = handler.dumpTemplates(); xerr != nil {
		return nil, xerr
	}

	templates, xerr := svc.ListTemplates(false)
	if xerr != nil {
		return nil, xerr
	}

	var templateNames []string
	for _, template := range templates {
		templateNames = append(templateNames, template.Name)
	}

	logrus.Infof("Starting scan of tenant %q with templates: %v", tenantName, templateNames)
	logrus.Infof("Using %q image", defaultScanImage)

	handler.scannedHostImage, xerr = svc.SearchImage(defaultScanImage)
	if xerr != nil {
		return nil, fail.Wrap(xerr, "could not find needed image in given service")
	}

	logrus.Infof("Creating scan network: %q", scanNetworkName)
	network, xerr := handler.getScanNetwork()
	if xerr != nil {
		return nil, fail.Wrap(xerr, "could not get/create the scan network")
	}
	//defer network.Delete(task)

	logrus.Infof("Creating scan subnet: %q", scanSubnetName)
	subnet, xerr := handler.getScanSubnet(network.GetID())
	if xerr != nil {
		return nil, fail.Wrap(xerr, "could not get/create the scan subnet")
	}
	//defer subnet.Delete(task)

	xerr = subnet.Inspect(task, func(clonable data.Clonable, _ *serialize.JSONProperties) fail.Error {
		handler.abstractSubnet = clonable.(*abstract.Subnet)
		return nil
	})
	if xerr != nil {
		return nil, xerr
	}

	var scanResultList []*protocol.ScanResult

	var scanWaitGroup sync.WaitGroup
	scanChannel := make(chan bool, int(math.Min(maxParallelScans, float64(len(templates)))))

	scanWaitGroup.Add(len(templates))

	for _, targetTemplate := range templates {
		scanChannel <- true
		localTarget := targetTemplate

		// TODO: If there is a file with today's date, skip it...
		fileCandidate := utils.AbsPathify("$HOME/.safescale/scanner/" + tenantName + "#" + localTarget.Name + ".json")
		if _, err := os.Stat(fileCandidate); !os.IsNotExist(err) {
			break
		}

		go func(innerTemplate abstract.HostTemplate) {
			logrus.Infof("Started scan for template %q", innerTemplate.Name)
			lerr := handler.analyzeTemplate(innerTemplate)
			if lerr != nil {
				logrus.Warnf("Error running scanner for template %q: %+v", innerTemplate.Name, lerr)
				scanResultList = append(scanResultList, &protocol.ScanResult{
					TemplateName: innerTemplate.Name,
					ScanSuccess:  false,
				})
			} else {
				scanResultList = append(scanResultList, &protocol.ScanResult{
					TemplateName: innerTemplate.Name,
					ScanSuccess:  true,
				})
			}
			<-scanChannel
			scanWaitGroup.Done()
		}(localTarget)
	}

	for i := 0; i < cap(scanChannel); i++ {
		scanChannel <- true
	}

	scanWaitGroup.Wait()

	if err := handler.collect(); err != nil {
		return nil, fail.Wrap(err, "failed to save scanned info for tenant '%s'", svc.GetName())
	}
	return &protocol.ScanResultList{Results: scanResultList}, nil
}

func (handler *tenantHandler) analyzeTemplate(template abstract.HostTemplate) (xerr fail.Error) {

	svc := handler.job.GetService()
	task := handler.job.GetTask()
	tenantName := svc.GetName()

	hostName := scannedHostPrefix + template.Name
	host, xerr := hostfactory.New(svc)
	if xerr != nil {
		return xerr
	}

	// Fix harcoded flexible engine host name regex
	if tenantName == "flexibleengine" {
		hostName = strings.ReplaceAll(hostName, ".", "_")
	}

	req := abstract.HostRequest{
		ResourceName: hostName,
		Subnets:      []*abstract.Subnet{handler.abstractSubnet},
		PublicIP:     false,
		TemplateID:   template.ID,
	}
	def := abstract.HostSizingRequirements{
		Image: defaultScanImage,
	}

	if _, xerr = host.Create(task, req, def); xerr != nil {
		return fail.Wrap(xerr, "template [%s] host '%s': error creation", template.Name, hostName)
	}

	defer func() {
		logrus.Infof("Deleting host '%s' with ID '%s'", hostName, host.GetID())
		derr := host.Delete(task)
		if derr != nil {
			logrus.Warnf("Error deleting host '%s'", hostName)
		}
	}()

	_, cout, _, xerr := host.Run(task, cmd, outputs.COLLECT, temporal.GetConnectionTimeout(), 8*time.Minute) // FIXME: hardcoded timeout
	if xerr != nil {
		return fail.Wrap(xerr, "template [%s] host '%s': failed to run collection script", template.Name, hostName)
	}
	daCPU, xerr := createCPUInfo(cout)
	if xerr != nil {
		return fail.Wrap(xerr, "template [%s]: Problem building cpu info", template.Name)
	}

	daCPU.TemplateName = template.Name
	daCPU.TemplateID = template.ID
	daCPU.ImageID = handler.scannedHostImage.ID
	daCPU.ImageName = handler.scannedHostImage.Name
	daCPU.TenantName = tenantName
	daCPU.LastUpdated = time.Now().Format(time.RFC850)

	daOut, err := json.MarshalIndent(daCPU, "", "\t")
	if err != nil {
		logrus.Warnf("tenant '%s', template '%s' : Problem marshaling json data: %v", tenantName, template.Name, err)
		return fail.ToError(err)
	}

	nerr := ioutil.WriteFile(utils.AbsPathify("$HOME/.safescale/scanner/"+tenantName+"#"+template.Name+".json"), daOut, 0666)
	if nerr != nil {
		logrus.Warnf("tenant '%s', template '%s' : Error writing file: %v", tenantName, template.Name, nerr)
		return fail.ToError(nerr)
	}
	logrus.Infof("tenant '%s', template '%s': Stored in file: %s", tenantName, template.Name, "$HOME/.safescale/scanner/"+tenantName+"#"+template.Name+".json")

	return nil
}

func (handler *tenantHandler) dryRun() (_ *protocol.ScanResultList, xerr fail.Error) {
	svc := handler.job.GetService()

	var resultList []*protocol.ScanResult

	templates, xerr := svc.ListTemplates(false)
	if xerr != nil {
		return nil, xerr
	}
	for _, template := range templates {
		resultList = append(resultList, &protocol.ScanResult{
			TemplateName: template.Name,
			ScanSuccess:  false,
		})
	}

	return &protocol.ScanResultList{Results: resultList}, xerr
}

func (handler *tenantHandler) checkScannable() (isScannable bool, xerr fail.Error) {
	svc := handler.job.GetService()

	params := svc.GetTenantParameters()

	compute, ok1 := params["compute"].(map[string]interface{})
	isScannable, ok2 := compute["Scannable"].(bool)

	if !(ok1 && ok2) {
		return false, fail.InvalidParameterError("scannable", "not set")
	}

	return isScannable, xerr
}

func (handler *tenantHandler) dumpTemplates() (xerr fail.Error) {
	err := os.MkdirAll(utils.AbsPathify("$HOME/.safescale/scanner"), 0777)
	if err != nil {
		return fail.ToError(err)
	}

	type TemplateList struct {
		Templates []abstract.HostTemplate `json:"templates,omitempty"`
	}

	svc := handler.job.GetService()
	templates, xerr := svc.ListTemplates(false)
	if xerr != nil {
		return xerr
	}

	content, err := json.Marshal(TemplateList{
		Templates: templates,
	})
	if xerr != nil {
		return fail.ToError(err)
	}

	f := fmt.Sprintf("$HOME/.safescale/scanner/%s-templates.json", svc.GetName())
	f = utils.AbsPathify(f)

	if err = ioutil.WriteFile(f, content, 0666); err != nil {
		return fail.ToError(err)
	}

	return nil
}

func (handler *tenantHandler) dumpImages() (xerr fail.Error) {
	if err := os.MkdirAll(utils.AbsPathify("$HOME/.safescale/scanner"), 0777); err != nil {
		return fail.ToError(err)
	}

	type ImageList struct {
		Images []abstract.Image `json:"images,omitempty"`
	}

	svc := handler.job.GetService()
	images, xerr := svc.ListImages(false)
	if xerr != nil {
		return xerr
	}

	content, err := json.Marshal(ImageList{
		Images: images,
	})
	if err != nil {
		return fail.ToError(err)
	}

	f := fmt.Sprintf("$HOME/.safescale/scanner/%s-images.json", svc.GetName())
	f = utils.AbsPathify(f)

	if err := ioutil.WriteFile(f, content, 0666); err != nil {
		return fail.ToError(err)
	}

	return nil
}

func (handler *tenantHandler) getScanNetwork() (network resources.Network, xerr fail.Error) {
	task := handler.job.GetTask()
	svc := handler.job.GetService()
	network, xerr = networkfactory.Load(task, svc, scanNetworkName)
	if xerr != nil {
		if _, ok := xerr.(*fail.ErrNotFound); !ok {
			return nil, xerr
		}

		network, xerr = networkfactory.New(svc)
		if xerr != nil {
			return nil, xerr
		}
		req := abstract.NetworkRequest{
			Name: scanNetworkName,
			CIDR: scanNetworkCIDR,
		}
		if xerr = network.Create(task, req); xerr != nil {
			return nil, xerr
		}
		return network, xerr
	}
	return network, xerr
}

func (handler *tenantHandler) getScanSubnet(networkID string) (subnet resources.Subnet, xerr fail.Error) {
	task := handler.job.GetTask()
	svc := handler.job.GetService()
	subnet, xerr = subnetfactory.Load(task, svc, scanNetworkName, scanSubnetName)
	if xerr != nil {
		if _, ok := xerr.(*fail.ErrNotFound); !ok {
			return nil, xerr
		}
		subnet, xerr = subnetfactory.New(svc)
		if xerr != nil {
			return nil, xerr
		}
		req := abstract.SubnetRequest{
			Name:      scanSubnetName,
			NetworkID: networkID,
			IPVersion: ipversion.IPv4,
			CIDR:      scanSubnetCIDR,
		}

		subnetHostSizing := abstract.HostSizingRequirements{
			MinGPU: -1,
		}
		if xerr = subnet.Create(task, req, "", &subnetHostSizing); xerr != nil {
			return nil, xerr
		}

		return subnet, xerr
	}
	return subnet, xerr
}

func createCPUInfo(output string) (_ *CPUInfo, xerr fail.Error) {
	str := strings.TrimSpace(output)

	tokens := strings.Split(str, "î")
	if len(tokens) < 9 {
		return nil, fail.SyntaxError("parsing error: '%s'", str)
	}
	info := CPUInfo{}
	var err error
	if info.NumberOfCPU, err = strconv.Atoi(tokens[0]); err != nil {
		return nil, fail.SyntaxError("parsing error: NumberOfCPU='%s' (from '%s')", tokens[0], str)
	}
	if info.NumberOfCore, err = strconv.Atoi(tokens[1]); err != nil {
		return nil, fail.SyntaxError("parsing error: NumberOfCore='%s' (from '%s')", tokens[1], str)
	}
	if info.NumberOfSocket, err = strconv.Atoi(tokens[2]); err != nil {
		return nil, fail.SyntaxError("parsing error: NumberOfSocket='%s' (from '%s')", tokens[2], str)
	}
	info.NumberOfCore *= info.NumberOfSocket
	if info.CPUFrequency, err = strconv.ParseFloat(tokens[3], 64); err != nil {
		return nil, fail.SyntaxError("parsing error: CpuFrequency='%s' (from '%s')", tokens[3], str)
	}
	info.CPUFrequency = math.Floor(info.CPUFrequency*100) / 100000

	info.CPUArch = tokens[4]
	info.Hypervisor = tokens[5]
	info.CPUModel = tokens[6]
	if info.RAMSize, err = strconv.ParseFloat(tokens[7], 64); err != nil {
		return nil, fail.SyntaxError("parsing error: RAMSize='%s' (from '%s')", tokens[7], str)
	}

	memInGb := info.RAMSize / 1024 / 1024
	info.RAMSize = math.Floor(memInGb*100) / 100
	if info.RAMFreq, err = strconv.ParseFloat(tokens[8], 64); err != nil {
		info.RAMFreq = 0
	}
	gpuTokens := strings.Split(tokens[9], "%")
	nb := len(gpuTokens)
	if nb > 1 {
		info.GPUModel = strings.TrimSpace(gpuTokens[0])
		info.GPU = nb - 1
	}

	if info.DiskSize, err = strconv.ParseInt(tokens[10], 10, 64); err != nil {
		info.DiskSize = 0
	}
	info.DiskSize = info.DiskSize / 1024 / 1024 / 1024

	if info.EphDiskSize, err = strconv.ParseInt(tokens[11], 10, 64); err != nil {
		info.EphDiskSize = 0
	}
	info.EphDiskSize = info.EphDiskSize / 1024 / 1024 / 1024

	if info.MainDiskSpeed, err = strconv.ParseFloat(tokens[12], 64); err != nil {
		info.MainDiskSpeed = 0
	}

	rotational, err := strconv.ParseInt(tokens[13], 10, 64)
	if err != nil {
		info.MainDiskType = ""
	} else {
		if rotational == 1 {
			info.MainDiskType = "HDD"
		} else {
			info.MainDiskType = "SSD"
		}
	}

	nsp, err := strconv.ParseFloat(tokens[14], 64)
	if err != nil {
		info.SampleNetSpeed = 0
	} else {
		info.SampleNetSpeed = nsp / 1000 / 8
	}

	info.PricePerHour = 0

	return &info, nil
}

func (handler *tenantHandler) collect() (xerr fail.Error) {
	svc := handler.job.GetService()

	authOpts, xerr := svc.GetAuthenticationOptions()
	if xerr != nil {
		return xerr
	}
	region, ok := authOpts.Get("Region")
	if !ok {
		return fail.InvalidRequestError("'Region' not set in tenant 'compute' section")
	}

	folder := fmt.Sprintf("images/%s/%s", svc.GetName(), region)

	if err := os.MkdirAll(utils.AbsPathify("$HOME/.safescale/scanner"), 0777); err != nil {
		return fail.ToError(err)
	}

	db, err := scribble.New(utils.AbsPathify("$HOME/.safescale/scanner/db"), nil)
	if err != nil {
		return fail.ToError(err)
	}

	files, err := ioutil.ReadDir(utils.AbsPathify("$HOME/.safescale/scanner"))
	if err != nil {
		return fail.ToError(err)
	}

	for _, file := range files {
		acpu := StoredCPUInfo{}
		theFile := utils.AbsPathify(fmt.Sprintf("$HOME/.safescale/scanner/%s", file.Name()))
		if strings.Contains(file.Name(), svc.GetName()+"#") {
			logrus.Infof("Storing: %s", file.Name())

			byteValue, err := ioutil.ReadFile(theFile)
			if err != nil {
				return fail.ToError(err)
			}

			if err = json.Unmarshal(byteValue, &acpu); err != nil {
				return fail.ToError(err)
			}

			acpu.ID = acpu.ImageID

			if err = db.Write(folder, acpu.TemplateName, acpu); err != nil {
				return fail.ToError(err)
			}
		}
		// Don't remove for testing
		//if !file.IsDir() {
		//	if err = os.Remove(theFile); err != nil {
		//		logrus.Infof("Error Supressing %s : %s", file.Name(), err.Error())
		//	}
		//}
	}
	return nil
}

func (handler *tenantHandler) Inspect(tenantName string) (_ *protocol.TenantInspectResponse, xerr fail.Error) {
	if handler == nil {
		return nil, fail.InvalidInstanceError()
	}
	if handler.job == nil {
		return nil, fail.InvalidInstanceContentError("handler.job", "cannot be nil")
	}
	if tenantName == "" {
		return nil, fail.InvalidParameterError("tenant name", "cannot be empty string")
	}

	tracer := debug.NewTracer(handler.job.GetTask(), tracing.ShouldTrace("handlers.tenant")).WithStopwatch().Entering()
	defer tracer.Exiting()
	defer fail.OnExitLogError(&xerr, tracer.TraceMessage())

	svc := handler.job.GetService()

	authOpts, xerr := svc.GetAuthenticationOptions()
	if xerr != nil {
		return nil, xerr
	}

	tenantParams := svc.GetTenantParameters()

	objectParams, _ := tenantParams["objectstorage"].(map[string]interface{})

	computeParams, ok1 := tenantParams["compute"].(map[string]interface{})
	_, ok2 := computeParams["CryptKey"].(string)

	var tenantMetadataCrypted bool
	if ok1 && ok2 {
		tenantMetadataCrypted = true
	}

	tenantCompute := protocol.TenantCompute{
		Region:           fmt.Sprint(computeParams["Region"]),
		SubRegion:        fmt.Sprint(computeParams["SubRegion"]),
		AvailabilityZone: fmt.Sprint(computeParams["AvailabilityZone"]),
		// TODO: add Context & ApiKey
		WhitelistTemplateRegex: fmt.Sprint(computeParams["WhitelistTemplateRegexp"]),
		BlacklistTemplateRegex: fmt.Sprint(computeParams["BlacklistTemplateRegexp"]),
		DefaultImage:           fmt.Sprint(computeParams["DefaultImage"]),
		DefaultVolumeSpeed:     fmt.Sprint(computeParams["DefaultVolumeSpeed"]),
		DnsList:                fmt.Sprint(computeParams["DnsList"]),
		OperatorUsername:       fmt.Sprint(computeParams["OperatorUsername"]),
	}

	tenantMetadata := protocol.TenantMetadata{
		Storage: &protocol.TenantObjectStorage{
			Type:      fmt.Sprint(objectParams["Type"]),
			Endpoint:  fmt.Sprint(objectParams["Endpoint"]),
			AuthUrl:   fmt.Sprint(objectParams["AuthURL"]),
			AccessKey: fmt.Sprint(objectParams["AccessKey"]),
			Region:    fmt.Sprint(objectParams["Region"]),
		},
		BucketName: svc.GetMetadataBucket().Name,
		Crypt:      tenantMetadataCrypted,
	}

	identParams, _ := tenantParams["identity"].(map[string]interface{})
	var idKeyValues []*protocol.KeyValue
	for key, value := range identParams {
		if isSecret, _ := regexp.MatchString(`(.*Password.*)|(.*Private.*)|(.*Secret.*)`, key); isSecret {
			continue
		}
		idKeyValues = append(idKeyValues, &protocol.KeyValue{
			Key:   key,
			Value: fmt.Sprint(value),
		})
	}
	tenantIdentity := protocol.TenantIdentity{
		KeyValues: idKeyValues,
	}

	region, ok := authOpts.Get("Region")
	if !ok {
		return nil, fail.InvalidRequestError("'Region' not set in tenant 'compute' section")
	}
	folder := fmt.Sprintf("images/%s/%s", svc.GetName(), region)

	db, err := scribble.New(utils.AbsPathify("$HOME/.safescale/scanner/db"), nil)
	if err != nil {
		return nil, fail.ToError(err)
	}

	templates, xerr := svc.ListTemplates(true)
	if xerr != nil {
		return nil, xerr
	}

	var scannedTemplateList []*protocol.HostTemplate

	for _, template := range templates {
		acpu := StoredCPUInfo{}
		if err := db.Read(folder, template.Name, &acpu); err != nil {
			scannedTemplateList = append(scannedTemplateList, &protocol.HostTemplate{
				Id:       template.ID,
				Name:     template.Name,
				Cores:    int32(template.Cores),
				Ram:      int32(template.RAMSize),
				Disk:     int32(template.DiskSize),
				GpuCount: int32(template.GPUNumber),
				GpuType:  template.GPUType,
			})
		} else {
			scannedTemplateList = append(scannedTemplateList, &protocol.HostTemplate{
				Id:       template.ID,
				Name:     template.Name,
				Cores:    int32(template.Cores),
				Ram:      int32(template.RAMSize),
				Disk:     int32(template.DiskSize),
				GpuCount: int32(template.GPUNumber),
				GpuType:  template.GPUType,
				Scanned: &protocol.ScannedInfo{
					TenantName:           acpu.TenantName,
					TemplateId:           acpu.ID,
					TemplateName:         acpu.TemplateName,
					ImageId:              acpu.ImageID,
					ImageName:            acpu.ImageName,
					LastUpdated:          acpu.LastUpdated,
					NumberOfCpu:          int64(acpu.NumberOfCPU),
					NumberOfCore:         int64(acpu.NumberOfCore),
					NumberOfSocket:       int64(acpu.NumberOfSocket),
					CpuFrequency_Ghz:     acpu.CPUFrequency,
					CpuArch:              acpu.CPUArch,
					Hypervisor:           acpu.Hypervisor,
					CpuModel:             acpu.CPUModel,
					RamSize_Gb:           acpu.RAMSize,
					RamFreq:              acpu.RAMFreq,
					Gpu:                  int64(acpu.GPU),
					GpuModel:             acpu.GPUModel,
					DiskSize_Gb:          acpu.DiskSize,
					MainDiskType:         acpu.MainDiskType,
					MainDiskSpeed_MBps:   acpu.MainDiskSpeed,
					SampleNetSpeed_KBps:  acpu.SampleNetSpeed,
					EphDiskSize_Gb:       acpu.EphDiskSize,
					PriceInDollarsSecond: acpu.PricePerSecond,
					PriceInDollarsHour:   acpu.PricePerHour,
					Prices: []*protocol.PriceInfo{{
						Currency:      "euro-fake",
						DurationLabel: "perMonth",
						Duration:      1,
						Price:         30,
					}},
				},
			})
		}
	}

	//TODO: complete with TenantIdentity and more details

	response := protocol.TenantInspectResponse{
		TenantName: tenantName,
		Provider:   svc.GetName(),
		Identity:   &tenantIdentity,
		Compute:    &tenantCompute,
		Metadata:   &tenantMetadata,
		Templates:  scannedTemplateList,
	}

	return &response, nil

}
