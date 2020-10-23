package providers

import (
	"fmt"
	"github.com/CS-SI/SafeScale/lib/utils/fail"
	"github.com/spf13/viper"
	"strings"
)

const (
	IdentitySection uint8 = iota
	ComputeSection
	ObjectStorageSection
	MetadataSection

	IdentitySectionLabel      = "identity"
	ComputeSectionLabel       = "compute"
	ObjectStorageSectionLabel = "objectstorage"
	MetadataSectionLabel      = "metadata"
)

var (
	mapSections = map[uint8]string{
		IdentitySection:      IdentitySectionLabel,
		ComputeSection:       ComputeSectionLabel,
		ObjectStorageSection: ObjectStorageSectionLabel,
		MetadataSection:      MetadataSectionLabel,
	}
	reversedMapSections = map[string]uint8{
		IdentitySectionLabel:      IdentitySection,
		ComputeSectionLabel:       ComputeSection,
		ObjectStorageSectionLabel: ObjectStorageSection,
		MetadataSectionLabel:      MetadataSection,
	}
)

// Keyword represents a keyword for the tenant
type Keyword struct {
	Label     string
	Aliases   []string
	Default   string // contains a default value for the keyword
	Validator string // should be a pointer to a function, but I don't know yet the API (using govalidator ?)
}

// Section represents a section from tenant settings
type Section struct {
	keywords    []Keyword           // contains the keywords the section accepts
	allAccepted map[string]*Keyword // contains a list of all Keywords indexed on all accepted strings (direct + aliases)
	values      map[*Keyword]string // values indexed on keywords
}

func NewSection(keywords []Keyword) (*Section, fail.Error) {
	s := &Section{
		keywords:    keywords,
		allAccepted: map[string]*Keyword{},
	}
	if xerr := s.init(); xerr != nil {
		return &Section{}, xerr
	}
	return s, nil
}

func (s *Section) init() fail.Error {
	for _, v := range s.keywords {
		label := strings.ToLower(strings.TrimSpace(v.Label))
		if _, ok := s.allAccepted[label]; ok {
			return fail.DuplicateError("keyword '%s' already registered", v.Label)
		}
		s.allAccepted[label] = &v
		for _, a := range v.Aliases {
			label := strings.ToLower(strings.TrimSpace(a))
			if _, ok := s.allAccepted[label]; ok {
				return fail.DuplicateError("keyword '%s' already registered", a)
			}
			s.allAccepted[label] = &v
		}
		s.values[&v] = v.Default
	}
	return nil
}

// ImportFromViper fills the section from viper
func (s *Section) ImportFromViper(v *viper.Viper, section string) fail.Error {

}

// Settings contains the settings of a tenant
type Settings struct {
	sections [4]*Section
}

func (s *Settings) ConfigureSection(section uint8, keywords []Keyword) (xerr fail.Error) {
	if int(section) > len(s.sections) {
		return fail.InvalidParameterError("section", fmt.Sprintf("value '%d' not supported", section))
	}
	if s.sections[section] != nil {
		return fail.DuplicateError("section '%s' (%d) already configured", mapSections[section], section)
	}
	if s.sections[section], xerr = NewSection(keywords); xerr != nil {
		return xerr
	}
	return nil
}

// ImportFromViper fills the tenant settings from file read by viper
func (s *Settings) ImportFromViper(v *viper.Viper) fail.Error {

	for idx, label := range mapSections {
		if xerr := s.sections[idx].ImportFromViper(v, label); xerr != nil {
			return xerr
		}
	}
	return nil
}
