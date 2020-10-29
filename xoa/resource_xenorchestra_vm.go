package xoa

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/ddelnano/terraform-provider-xenorchestra/client"
	"github.com/hashicorp/terraform-plugin-sdk/helper/hashcode"
	"github.com/hashicorp/terraform-plugin-sdk/helper/schema"
)

func init() {
}

var validHaOptions = []string{
	"",
	"best-effort",
	"restart",
}

func StringInSlice(valid []string, ignoreCase bool) schema.SchemaValidateFunc {
	return func(i interface{}, k string) (s []string, es []error) {
		v, ok := i.(string)
		if !ok {
			es = append(es, fmt.Errorf("expected type of %s to be string", k))
			return
		}

		for _, str := range valid {
			if v == str || (ignoreCase && strings.ToLower(v) == strings.ToLower(str)) {
				return
			}
		}

		es = append(es, fmt.Errorf("expected %s to be one of %v, got %s", k, valid, v))
		return
	}
}

func resourceRecord() *schema.Resource {
	duration := 5 * time.Minute
	return &schema.Resource{
		Create: resourceVmCreate,
		Read:   resourceVmRead,
		Update: resourceVmUpdate,
		Delete: resourceVmDelete,
		Importer: &schema.ResourceImporter{
			State: RecordImport,
		},
		Timeouts: &schema.ResourceTimeout{
			Create: &duration,
			Update: &duration,
		},
		Schema: map[string]*schema.Schema{
			"name_label": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
			},
			"name_description": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"auto_poweron": &schema.Schema{
				Type:     schema.TypeBool,
				Default:  false,
				Optional: true,
			},
			"high_availability": &schema.Schema{
				Type:     schema.TypeString,
				Default:  "",
				Optional: true,
				// TODO: Replace with validation.StringInSlice when terraform
				// and the SDK are upgraded.
				ValidateFunc: StringInSlice(validHaOptions, false),
			},
			"template": &schema.Schema{
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"cloud_config": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"core_os": &schema.Schema{
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},
			"cpu_cap": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
				Default:  0,
			},
			"cpu_weight": &schema.Schema{
				Type:     schema.TypeInt,
				Optional: true,
				Default:  0,
			},
			"cpus": &schema.Schema{
				Type:     schema.TypeInt,
				Required: true,
			},
			"memory_max": &schema.Schema{
				Type:     schema.TypeInt,
				Required: true,
			},
			"resource_set": &schema.Schema{
				Type:     schema.TypeString,
				Optional: true,
			},
			"network": &schema.Schema{
				Type:     schema.TypeSet,
				Required: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"attached": &schema.Schema{
							Type:     schema.TypeBool,
							Default:  true,
							Optional: true,
						},
						"device": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
						},
						"network_id": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
						},
						"mac_address": &schema.Schema{
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
							// ForceNew: true,
							ValidateFunc: func(val interface{}, key string) (warns []string, errs []error) {
								mac_address := val.(string)
								if _, err := net.ParseMAC(mac_address); err != nil {
									errs = append(errs, fmt.Errorf("%s Mac Address is invalid", mac_address))
								}
								return

							},
						},
					},
				},
				// Set: func(v interface{}) int {
				// 	vif := v.(map[string]interface{})
				// 	device := vif["device"].(string)
				// 	return hashcode.String(device)
				// },
			},
			"disk": &schema.Schema{
				Type:     schema.TypeSet,
				Required: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"sr_id": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
						},
						"name_label": &schema.Schema{
							Type:     schema.TypeString,
							Required: true,
						},
						"size": &schema.Schema{
							Type:     schema.TypeInt,
							Required: true,
						},
					},
				},
				Set: func(value interface{}) int {
					var buf bytes.Buffer
					disk := value.(map[string]interface{})

					buf.WriteString(fmt.Sprintf("%s-", disk["sr_id"].(string)))
					buf.WriteString(fmt.Sprintf("%s-", disk["name_label"].(string)))
					buf.WriteString(fmt.Sprintf("%d-", disk["size"]))
					return hashcode.String(buf.String())
				},
			},
		},
		CustomizeDiff: func(diff *schema.ResourceDiff, v interface{}) error {
			for _, key := range diff.GetChangedKeysPrefix("") {
				old, new := diff.GetChange(key)
				log.Printf("[DEBUG] Seeing diff for key: %s with before: %+v and after: %+v and from Get(): %+v\n", key, old, new, diff.Get(key))
			}
			return nil
		},
	}
}

func resourceVmCreate(d *schema.ResourceData, m interface{}) error {
	c := m.(*client.Client)

	network_maps := []map[string]string{}
	networks := d.Get("network").(*schema.Set)

	for _, network := range networks.List() {
		net, _ := network.(map[string]interface{})

		network_maps = append(network_maps, map[string]string{
			"network_id":  net["network_id"].(string),
			"mac_address": net["mac_address"].(string),
		})
	}

	vdis := []client.VDI{}

	disks := d.Get("disk").(*schema.Set)

	for _, disk := range disks.List() {
		vdi, _ := disk.(map[string]interface{})

		vdis = append(vdis, client.VDI{
			SrId:      vdi["sr_id"].(string),
			NameLabel: vdi["name_label"].(string),
			Size:      vdi["size"].(int),
		})
	}

	vm, err := c.CreateVm(
		d.Get("name_label").(string),
		d.Get("name_description").(string),
		d.Get("template").(string),
		d.Get("cloud_config").(string),
		d.Get("resource_set").(string),
		d.Get("cpus").(int),
		d.Get("memory_max").(int),
		network_maps,
		vdis,
	)

	if err != nil {
		return err
	}

	d.SetId(vm.Id)
	d.Set("cloud_config", d.Get("cloud_config").(string))
	d.Set("memory_max", d.Get("memory_max").(int))
	d.Set("resource_set", d.Get("resource_set").(string))

	vifs, err := c.GetVIFs(vm)

	if err != nil {
		return err
	}

	err = d.Set("network", vifsToMapList(vifs))
	fmt.Printf("Tried setting network state and received error: %v", err)

	if err != nil {
		return err
	}
	fmt.Printf("The network state looks like this: %+v and the full state is: %+v\n", d.Get("network"), d.State())
	return nil
}

func vifsToMapList(vifs []client.VIF) []interface{} {
	result := make([]interface{}, 0, len(vifs))
	for _, vif := range vifs {
		vifMap := map[string]interface{}{
			"attached":    vif.Attached,
			"device":      vif.Device,
			"mac_address": vif.MacAddress,
			"network_id":  vif.Network,
		}
		result = append(result, vifMap)
	}

	return result
}

func resourceVmRead(d *schema.ResourceData, m interface{}) error {
	c := m.(*client.Client)

	vm, err := c.GetVm(client.Vm{Id: d.Id()})

	if _, ok := err.(client.NotFound); ok {
		d.SetId("")
		return nil
	}

	if err != nil {
		return err
	}

	vifs, err := c.GetVIFs(vm)
	log.Printf("[DEBUG] Found VIFs for vm: %v\n", vifs)

	if err != nil {
		return err
	}
	recordToData(*vm, vifs, d)
	return nil
}

func resourceVmUpdate(d *schema.ResourceData, m interface{}) error {
	c := m.(*client.Client)

	nameLabel := d.Get("name_label").(string)
	nameDescription := d.Get("name_description").(string)
	cpus := d.Get("cpus").(int)
	autoPowerOn := d.Get("auto_poweron").(bool)
	ha := d.Get("high_availability").(string)
	rs := d.Get("resource_set").(string)
	vm, err := c.UpdateVm(d.Id(), cpus, nameLabel, nameDescription, ha, rs, autoPowerOn)
	log.Printf("[DEBUG] Retrieved vm after update: %+v\n", vm)

	if err != nil {
		return err
	}

	if d.HasChange("network") {
		origNet, newNet := d.GetChange("network")

		origNetSet := origNet.(*schema.Set)
		newNetSet := newNet.(*schema.Set)

		additions := expandNetworks(newNetSet.Difference(origNetSet).List())
		log.Printf("Found the following additions: %v from old: %v new: %v\n", newNetSet.Difference(origNetSet).List(), origNetSet, newNetSet)
		for _, addition := range additions {
			updateVif := shouldUpdateVif(*addition, expandNetworks(origNetSet.List()))

			if updateVif {
				if addition.Attached {
					c.ConnectVIF(addition)
				} else {
					c.DisconnectVIF(addition)
				}
			} else {
				_, vifErr := c.CreateVIF(vm, addition)

				if vifErr != nil {
					return err
				}
			}
		}

		removals := expandNetworks(origNetSet.Difference(newNetSet).List())

		for _, removal := range removals {
			vifErr := c.DeleteVIF(removal)

			if vifErr != nil {
				return err
			}
		}
	}

	return resourceVmRead(d, m)
}

func resourceVmDelete(d *schema.ResourceData, m interface{}) error {
	c := m.(*client.Client)

	err := c.DeleteVm(d.Id())

	if err != nil {
		return err
	}
	d.SetId("")
	return nil
}

func expandNetworks(networks []interface{}) []*client.VIF {
	vifs := make([]*client.VIF, 0, len(networks))
	for _, net := range networks {
		data := net.(map[string]interface{})

		attached := data["attached"].(bool)
		device := data["device"].(string)
		networkId := data["network_id"].(string)
		macAddress := data["mac_address"].(string)
		vifs = append(vifs, &client.VIF{
			Attached:   attached,
			Device:     device,
			Network:    networkId,
			MacAddress: macAddress,
		})
	}
	return vifs
}

func RecordImport(d *schema.ResourceData, m interface{}) ([]*schema.ResourceData, error) {
	c := m.(*client.Client)

	vm, err := c.GetVm(client.Vm{Id: d.Id()})
	if err != nil {
		return nil, err
	}

	rd := []*schema.ResourceData{d}
	vifs, err := c.GetVIFs(vm)

	if err != nil {
		return rd, err
	}
	recordToData(*vm, vifs, d)

	return rd, nil
}

func recordToData(resource client.Vm, vifs []client.VIF, d *schema.ResourceData) error {
	d.SetId(resource.Id)
	// d.Set("cloud_config", resource.CloudConfig)
	// err := d.Set("memory_max", resource.Memory.Size)
	// log.Printf("[DEBUG] Found error when setting memory_max %+v", err)

	// if err != nil {
	// 	return err
	// }
	d.Set("cpus", resource.CPUs.Number)
	d.Set("name_label", resource.NameLabel)
	d.Set("name_description", resource.NameDescription)
	d.Set("high_availability", resource.HA)
	d.Set("auto_poweron", resource.AutoPoweron)
	d.Set("resource_set", resource.ResourceSet)

	nets := vifsToMapList(vifs)
	err := d.Set("network", nets)
	fmt.Printf("Tried setting network state and received error: %v\n", err)

	if err != nil {
		return err
	}
	fmt.Printf("The network state looks like this: %+v and the full state is: %+v\n", d.Get("network"), d.State())
	return nil
}

func shouldUpdateVif(vif client.VIF, vifs []*client.VIF) bool {
	found := false
	vifCopy := vif
	var vifFound client.VIF
	for _, vifToCheck := range vifs {
		if vifToCheck.Id == vifCopy.Id || vifToCheck.MacAddress == vifCopy.MacAddress {
			found = true
			vifFound = *vifToCheck
		}
	}

	if !found {
		return false
	}

	vifCopy.Attached = !vifCopy.Attached
	if vifHash(vifCopy, true) == vifHash(vifFound, true) {
		return true
	}

	return false
}

func vifHash(value interface{}, withAttached bool) int {
	var macAddress string
	var networkId string
	var device string
	var attached bool
	switch t := value.(type) {
	case client.VIF:
		macAddress = t.MacAddress
		networkId = t.Network
		device = t.Device
		attached = t.Attached
	case map[string]interface{}:
		network := value.(map[string]interface{})
		macAddress = network["mac_address"].(string)
		networkId = network["network_id"].(string)
		device = network["device"].(string)
		attached = network["attached"].(bool)
	default:
		panic(fmt.Sprintf("can't has type %T", t))
	}

	var v string
	if withAttached {
		v = fmt.Sprintf("%s-%s-%s-%t", macAddress, networkId, device, attached)
	} else {
		v = fmt.Sprintf("%s-%s-%s", macAddress, networkId, device)
	}
	log.Printf("[DEBUG] Setting network via %s\n", v)

	return hashcode.String(v)
}
