package main

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strings"

	"encoding/hex"

	"github.com/hashicorp/terraform/helper/schema"
)

// ConfigXMLTemplate represents a config.xml template as an object.
type ConfigXMLTemplate struct {
	source string
	data   string
	hash   string
}

// NewConfigXMLTemplate creates a new ConfigXMLTemplate using the provided
// address or inline/embedded data.
func NewConfigXMLTemplate(input string) (*ConfigXMLTemplate, error) {

	configuration := &ConfigXMLTemplate{}
	var source string

	// extract data and hash, if the hash is there
	re := regexp.MustCompile(`.*@[a-f0-9]{32}$`)
	if re.MatchString(input) {
		source = input[:len(input)-33]
		configuration.hash = input[len(input)-32:]
	} else {
		source = input
	}

	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		log.Printf("[DEBUG] jenkins::xml - retrieving template from URL %q", source)
		response, err := http.Get(source)
		if err != nil {
			log.Printf("[ERROR] jenkins::xml - error connecting to HTTP server: %v", err)
			return nil, err
		}
		defer response.Body.Close()
		data, err := ioutil.ReadAll(response.Body)
		if err != nil {
			log.Printf("[ERROR] jenkins::xml - error reading HTTP server response: %v", err)
			return nil, err
		}
		configuration.source = source
		configuration.data = string(data)
	} else if strings.HasPrefix(source, "file://") {
		log.Printf("[DEBUG] jenkins::xml - retrieving template from filesystem: %q", source)
		from := strings.Replace(source, "file://", "", 1)
		data, err := ioutil.ReadFile(from)
		if err != nil {
			log.Printf("[ERROR] jenkins::xml - error reading from filesystem: %v", err)
			return nil, err
		}
		configuration.source = source
		configuration.data = string(data)
	} else {
		log.Printf("[DEBUG] jenkins::xml - template is inline: %q", source)
		configuration.source = ""
		configuration.data = source
	}
	return configuration, nil
}

func (c *ConfigXMLTemplate) GetTemplateID() (string, error) {
	if c == nil {
		log.Printf("[ERROR] jenkins::xml - invalid config.xml template object")
		return "", fmt.Errorf("Invalid config.xml template object")
	}

	if len(c.source) == 0 {
		// inline template
		return c.data, nil
	} else {
		// indirect template
		hash, _ := c.ComputedHash()
		return fmt.Sprintf("%s@%s", c.source, hash), nil
	}
}

// RecordedHash returns the hash as recorded in the original input, if available.
func (c *ConfigXMLTemplate) RecordedHash() (string, error) {
	if c == nil {
		log.Printf("[ERROR] jenkins::xml - invalid config.xml template object")
		return "", fmt.Errorf("Invalid config.xml template object")
	}

	return c.hash, nil
}

// ComputedHash returns the SHA-256 hash of the current (unbound) template.
func (c *ConfigXMLTemplate) ComputedHash() (string, error) {
	if c == nil {
		log.Printf("[ERROR] jenkins::xml - invalid config.xml template object")
		return "", fmt.Errorf("Invalid config.xml template object")
	}

	//hash := sha512.Sum512([]byte(c.template))
	hash := md5.Sum([]byte(c.data))
	return strings.ToLower(hex.EncodeToString(hash[:])), nil
}

// BindTo binds the current config.xml template to the given resource data.
func (c *ConfigXMLTemplate) BindTo(d *schema.ResourceData) (string, error) {

	if c == nil {
		log.Printf("[ERROR] jenkins::xml - invalid config.xml template object")
		return "", fmt.Errorf("Invalid config.xml template object")
	}

	log.Printf("[DEBUG] jenkins::xml - binding template:\n%s", c.data)

	// create and parse the config.xml template
	tpl, err := template.New("template").Parse(c.data)
	if err != nil {
		log.Printf("[ERROR] jenkins::xml - error parsing template: %v", err)
		return "", err
	}

	// Job contains all the data pertaining to a Jenkins job, in a format that is
	// easy to use with Golang text/templates
	type job struct {
		Name                      string
		Description               string
		DisplayName               string
		TriggerRemotelyToken      string
		Disabled                  bool
		MasterMergeTriggering     bool
		Permissions               []string
		Parameters                []map[string]string
		BranchPushTriggering      map[string]string
		PrTriggeringGhpr          map[string]string
		PrTriggeringGhIntegration map[string]string
		Jenkinsfile               map[string]string
		Configuration             map[string]string
	}

	// now copy the input parameters into a data structure that is compatible
	// with the config.xml template
	j := &job{
		Name:                      d.Get("name").(string),
		Permissions:               []string{},
		Configuration:             map[string]string{},
		Jenkinsfile:               map[string]string{},
		Parameters:                []map[string]string{},
		PrTriggeringGhpr:          map[string]string{},
		PrTriggeringGhIntegration: map[string]string{},
		BranchPushTriggering:      map[string]string{},
		MasterMergeTriggering:     false,
	}
	if value, ok := d.GetOk("display_name"); ok {
		j.DisplayName = value.(string)
	}
	if value, ok := d.GetOk("description"); ok {
		j.Description = value.(string)
	}
	if value, ok := d.GetOk("trigger_remotely_token"); ok {
		j.TriggerRemotelyToken = value.(string)
	}
	if value, ok := d.GetOk("disabled"); ok {
		j.Disabled = value.(bool)
	}
	if value, ok := d.GetOk("master_merge_triggering"); ok {
		j.MasterMergeTriggering = value.(bool)
	}
	if value, ok := d.GetOk("permissions"); ok {
		value := value.(string)
		elems := strings.Split(value, ",")
		for _, v := range elems {
			j.Permissions = append(j.Permissions, v)
		}
	}
	if value, ok := d.GetOk("configuration"); ok {
		value := value.(map[string]interface{})
		for k, v := range value {
			j.Configuration[k] = v.(string)
		}
	}
	if value, ok := d.GetOk("pr_triggering_ghpr"); ok {
		value := value.(map[string]interface{})
		for k, v := range value {
			j.PrTriggeringGhpr[k] = v.(string)
		}
	}
	if value, ok := d.GetOk("pr_triggering_gh_integration"); ok {
		value := value.(map[string]interface{})
		for k, v := range value {
			j.PrTriggeringGhIntegration[k] = v.(string)
		}
	}
	if value, ok := d.GetOk("parameter"); ok {
		fmt.Println(value)
		rawValue := value.([]interface{})

		for _, v := range rawValue {
			configRaw := v.(map[string]interface{})
			config := make(map[string]string)

			if t, ok := configRaw["type"]; ok {
				config["Type"] = t.(string)
			}

			if name, ok := configRaw["name"]; ok {
				config["Name"] = name.(string)
			}

			if description, ok := configRaw["description"]; ok {
				config["Description"] = description.(string)
			}

			if d, ok := configRaw["default"]; ok {
				config["Default"] = d.(string)
			}

			j.Parameters = append(j.Parameters, config)
		}
	}
	if value, ok := d.GetOk("branch_push_triggering"); ok {
		value := value.(map[string]interface{})
		for k, v := range value {
			j.BranchPushTriggering[k] = v.(string)
		}
	}
	if value, ok := d.GetOk("jenkinsfile"); ok {
		value := value.(map[string]interface{})
		for k, v := range value {
			j.Jenkinsfile[k] = v.(string)
		}
	}

	// apply the job object to the template
	var buffer bytes.Buffer
	err = tpl.Execute(&buffer, j)
	if err != nil {
		log.Printf("[ERROR] jenkis::xml - error executing template: %v", err)
		return "", err
	}

	xml := buffer.String()
	log.Printf("[DEBUG] jenkins::xml - bound template:\n%s", xml)
	return xml, nil
}
