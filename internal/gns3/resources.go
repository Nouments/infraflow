package gns3

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Compute is a controller compute entry. Passwords are never decoded.
type Compute struct {
	ComputeID string `json:"compute_id"`
	Name      string `json:"name"`
	Protocol  string `json:"protocol,omitempty"`
	Host      string `json:"host,omitempty"`
	Port      int    `json:"port,omitempty"`
	Connected *bool  `json:"connected,omitempty"`
}

// Template is a node template. Image fields are copied as reported; whether
// the image file exists is checked against Images, not assumed.
type Template struct {
	TemplateID   string  `json:"template_id"`
	Name         string  `json:"name"`
	TemplateType string  `json:"template_type"`
	Category     string  `json:"category,omitempty"`
	Builtin      bool    `json:"builtin"`
	ComputeID    string  `json:"compute_id,omitempty"`
	Image        string  `json:"image,omitempty"`
	HDADiskImage string  `json:"hda_disk_image,omitempty"`
	Path         string  `json:"path,omitempty"`
	RAM          int     `json:"ram,omitempty"`
	CPUs         float64 `json:"cpus,omitempty"`
	Adapters     int     `json:"adapters,omitempty"`
}

// ImageName returns the image a template depends on, if any.
func (template Template) ImageName() string {
	for _, value := range []string{template.HDADiskImage, template.Image, template.Path} {
		if value != "" {
			return value
		}
	}
	return ""
}

// Image is an image registered with the controller.
type Image struct {
	Filename  string `json:"filename"`
	ImageType string `json:"image_type"`
	ImageSize int64  `json:"image_size"`
	Checksum  string `json:"checksum,omitempty"`
}

// Project is a controller project.
type Project struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	AutoOpen  bool   `json:"auto_open"`
	AutoStart bool   `json:"auto_start"`
	AutoClose bool   `json:"auto_close"`
}

// Port is one adapter/port of a node.
type Port struct {
	Name          string `json:"name"`
	AdapterNumber int    `json:"adapter_number"`
	PortNumber    int    `json:"port_number"`
	LinkType      string `json:"link_type,omitempty"`
}

// Node is a project node.
type Node struct {
	NodeID      string `json:"node_id"`
	Name        string `json:"name"`
	NodeType    string `json:"node_type"`
	Status      string `json:"status"`
	ComputeID   string `json:"compute_id,omitempty"`
	TemplateID  string `json:"template_id,omitempty"`
	Console     *int   `json:"console,omitempty"`
	ConsoleHost string `json:"console_host,omitempty"`
	ConsoleType string `json:"console_type,omitempty"`
	Ports       []Port `json:"ports,omitempty"`
}

// LinkEnd is one side of a link.
type LinkEnd struct {
	NodeID        string `json:"node_id"`
	AdapterNumber int    `json:"adapter_number"`
	PortNumber    int    `json:"port_number"`
}

// Link is a project link.
type Link struct {
	LinkID          string    `json:"link_id,omitempty"`
	Nodes           []LinkEnd `json:"nodes"`
	Capturing       bool      `json:"capturing,omitempty"`
	CaptureFileName string    `json:"capture_file_name,omitempty"`
	CaptureFilePath string    `json:"capture_file_path,omitempty"`
	Suspend         bool      `json:"suspend,omitempty"`
	LinkType        string    `json:"link_type,omitempty"`
}

// list decodes a JSON array. On any error it returns nil: a partially
// decoded list must never be mistaken for the controller's real content.
func list[T any](ctx context.Context, client *Client, path string) ([]T, error) {
	var items []T
	if err := client.call(ctx, http.MethodGet, path, nil, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (client *Client) Computes(ctx context.Context) ([]Compute, error) {
	return list[Compute](ctx, client, "/computes")
}

func (client *Client) Templates(ctx context.Context) ([]Template, error) {
	return list[Template](ctx, client, "/templates")
}

// Images lists images. It is v3-only; on v2 it returns ErrNotFound.
func (client *Client) Images(ctx context.Context) ([]Image, error) {
	return list[Image](ctx, client, "/images")
}

// DockerImages lists the Docker images present on a compute, through the
// controller.
func (client *Client) DockerImages(ctx context.Context, computeID string) ([]string, error) {
	items, err := list[struct {
		Image string `json:"image"`
	}](ctx, client, "/computes/"+url.PathEscape(computeID)+"/docker/images")
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Image)
	}
	return names, nil
}

func (client *Client) Projects(ctx context.Context) ([]Project, error) {
	return list[Project](ctx, client, "/projects")
}

// FindProject returns the project with this exact name, or ErrNotFound.
func (client *Client) FindProject(ctx context.Context, name string) (Project, error) {
	projects, err := client.Projects(ctx)
	if err != nil {
		return Project{}, err
	}
	for _, project := range projects {
		if project.Name == name {
			return project, nil
		}
	}
	return Project{}, fmt.Errorf("%w: project %q", ErrNotFound, name)
}

// CreateProject creates a project that never auto-opens or auto-starts, so a
// controller restart does not boot lab devices behind the operator's back.
func (client *Client) CreateProject(ctx context.Context, name string) (Project, error) {
	var project Project
	payload := map[string]any{"name": name, "auto_open": false, "auto_start": false, "auto_close": true}
	return project, client.call(ctx, http.MethodPost, "/projects", payload, &project)
}

func (client *Client) OpenProject(ctx context.Context, projectID string) (Project, error) {
	var project Project
	return project, client.call(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectID)+"/open", map[string]any{}, &project)
}

func (client *Client) CloseProject(ctx context.Context, projectID string) error {
	return client.call(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectID)+"/close", map[string]any{}, nil)
}

func (client *Client) Nodes(ctx context.Context, projectID string) ([]Node, error) {
	return list[Node](ctx, client, "/projects/"+url.PathEscape(projectID)+"/nodes")
}

func (client *Client) Links(ctx context.Context, projectID string) ([]Link, error) {
	return list[Link](ctx, client, "/projects/"+url.PathEscape(projectID)+"/links")
}

// CreateNodeFromTemplate adds a node built from an existing template.
func (client *Client) CreateNodeFromTemplate(ctx context.Context, projectID, templateID, name string, x, y int) (Node, error) {
	var node Node
	payload := map[string]any{"x": x, "y": y, "name": name}
	path := "/projects/" + url.PathEscape(projectID) + "/templates/" + url.PathEscape(templateID)
	return node, client.call(ctx, http.MethodPost, path, payload, &node)
}

// UpdateNode changes node settings (PUT). The node must be stopped for most
// hardware properties.
func (client *Client) UpdateNode(ctx context.Context, projectID, nodeID string, changes map[string]any) (Node, error) {
	var node Node
	path := "/projects/" + url.PathEscape(projectID) + "/nodes/" + url.PathEscape(nodeID)
	return node, client.call(ctx, http.MethodPut, path, changes, &node)
}

// CreateLink connects two node ports.
func (client *Client) CreateLink(ctx context.Context, projectID string, a, b LinkEnd) (Link, error) {
	var link Link
	return link, client.call(ctx, http.MethodPost, "/projects/"+url.PathEscape(projectID)+"/links", Link{Nodes: []LinkEnd{a, b}}, &link)
}

func (client *Client) nodeAction(ctx context.Context, projectID, nodeID, action string) error {
	path := "/projects/" + url.PathEscape(projectID) + "/nodes/" + url.PathEscape(nodeID) + "/" + action
	return client.call(ctx, http.MethodPost, path, map[string]any{}, nil)
}

func (client *Client) StartNode(ctx context.Context, projectID, nodeID string) error {
	return client.nodeAction(ctx, projectID, nodeID, "start")
}

func (client *Client) StopNode(ctx context.Context, projectID, nodeID string) error {
	return client.nodeAction(ctx, projectID, nodeID, "stop")
}

func (client *Client) ReloadNode(ctx context.Context, projectID, nodeID string) error {
	return client.nodeAction(ctx, projectID, nodeID, "reload")
}

// StartCapture starts a packet capture on a link; the pcap stays on the
// controller host at the returned path.
func (client *Client) StartCapture(ctx context.Context, projectID, linkID string) (Link, error) {
	var link Link
	path := "/projects/" + url.PathEscape(projectID) + "/links/" + url.PathEscape(linkID) + "/capture/start"
	return link, client.call(ctx, http.MethodPost, path, map[string]any{}, &link)
}

func (client *Client) StopCapture(ctx context.Context, projectID, linkID string) error {
	path := "/projects/" + url.PathEscape(projectID) + "/links/" + url.PathEscape(linkID) + "/capture/stop"
	return client.call(ctx, http.MethodPost, path, map[string]any{}, nil)
}

// WaitNodeStatus polls until the node reports the wanted status.
func (client *Client) WaitNodeStatus(ctx context.Context, projectID, nodeID, status string, interval time.Duration) (Node, error) {
	for {
		nodes, err := client.Nodes(ctx, projectID)
		if err != nil {
			return Node{}, err
		}
		for _, node := range nodes {
			if node.NodeID == nodeID && node.Status == status {
				return node, nil
			}
		}
		select {
		case <-ctx.Done():
			return Node{}, fmt.Errorf("node %s did not reach %q: %w", nodeID, status, ctx.Err())
		case <-time.After(interval):
		}
	}
}

// TemplateAvailability says whether a template can actually be instantiated.
type TemplateAvailability struct {
	Template
	ImageRegistered *bool  `json:"image_registered,omitempty"`
	Note            string `json:"note,omitempty"`
}

// Inventory is a point-in-time view of the controller. A section that could
// not be read is listed in Errors; it is never filled with guessed data.
type Inventory struct {
	CollectedAt time.Time              `json:"collected_at"`
	URL         string                 `json:"url"`
	Version     Version                `json:"version"`
	Exposure    Exposure               `json:"exposure"`
	Computes    []Compute              `json:"computes"`
	Templates   []TemplateAvailability `json:"templates"`
	Images      []Image                `json:"images"`
	Projects    []Project              `json:"projects"`
	Errors      map[string]string      `json:"errors,omitempty"`
}

// CollectInventory reads version, computes, templates, images and projects.
func (client *Client) CollectInventory(ctx context.Context, includeBuiltin bool) (Inventory, error) {
	inventory := Inventory{CollectedAt: time.Now().UTC(), URL: client.BaseURL(), Exposure: client.Exposure(), Errors: map[string]string{}}
	version, err := client.Version(ctx)
	if err != nil {
		return inventory, err
	}
	inventory.Version = version
	if inventory.Computes, err = client.Computes(ctx); err != nil {
		inventory.Errors["computes"] = err.Error()
	}
	images, err := client.Images(ctx)
	registered := map[string]bool{}
	if err != nil {
		inventory.Errors["images"] = err.Error()
	} else {
		inventory.Images = images
		for _, image := range images {
			registered[image.Filename] = true
		}
	}
	templates, err := client.Templates(ctx)
	if err != nil {
		inventory.Errors["templates"] = err.Error()
	}
	dockerImages := map[string]map[string]bool{}
	dockerPresent := func(computeID, image string) (bool, error) {
		if computeID == "" {
			computeID = "local"
		}
		if _, ok := dockerImages[computeID]; !ok {
			names, err := client.DockerImages(ctx, computeID)
			if err != nil {
				return false, err
			}
			dockerImages[computeID] = map[string]bool{}
			for _, name := range names {
				dockerImages[computeID][name] = true
			}
		}
		if !strings.Contains(image, ":") {
			image += ":latest"
		}
		return dockerImages[computeID][image], nil
	}
	for _, template := range templates {
		if template.Builtin && !includeBuiltin {
			continue
		}
		entry := TemplateAvailability{Template: template}
		switch name := template.ImageName(); {
		case name == "":
		case template.TemplateType == "docker":
			found, err := dockerPresent(template.ComputeID, name)
			if err != nil {
				entry.Note = "docker image presence unknown: " + err.Error()
				break
			}
			entry.ImageRegistered = &found
			if !found {
				entry.Note = "docker image is not present on the compute"
			}
		case images != nil:
			found := registered[name] || registered[baseName(name)]
			entry.ImageRegistered = &found
			if !found {
				entry.Note = "template image is not registered with the controller"
			}
		}
		inventory.Templates = append(inventory.Templates, entry)
	}
	sort.Slice(inventory.Templates, func(i, j int) bool { return inventory.Templates[i].Name < inventory.Templates[j].Name })
	if inventory.Projects, err = client.Projects(ctx); err != nil {
		inventory.Errors["projects"] = err.Error()
	}
	if len(inventory.Errors) == 0 {
		inventory.Errors = nil
	}
	return inventory, nil
}

func baseName(path string) string {
	for index := len(path) - 1; index >= 0; index-- {
		if path[index] == '/' || path[index] == '\\' {
			return path[index+1:]
		}
	}
	return path
}
