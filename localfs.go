package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	pkgmodel "github.com/platform-engineering-labs/formae/pkg/model"
	"github.com/platform-engineering-labs/formae/pkg/plugin"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

const (
	ResourceTypeDirectory = "LOCALFS::FS::Directory"
	defaultDirPerm        = "0755"
)

type TargetConfig struct {
	BasePath string `json:"BasePath"`
}

type DirectoryProperties struct {
	Path        string `json:"path"`
	Permissions string `json:"permissions"`
}

type Plugin struct{}

var _ plugin.ResourcePlugin = &Plugin{}

func parseTargetConfig(data json.RawMessage) (*TargetConfig, error) {
	var cfg TargetConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid target config: %w", err)
	}
	if cfg.BasePath == "" {
		return nil, fmt.Errorf("BasePath is required")
	}
	return &cfg, nil
}

func parsePermissions(s string) (os.FileMode, error) {
	var perm os.FileMode
	n, err := fmt.Sscanf(s, "%o", &perm)
	if err != nil || n != 1 {
		return 0, fmt.Errorf("invalid permissions %q", s)
	}
	return perm, nil
}

func formatPerm(mode os.FileMode) string {
	return fmt.Sprintf("%04o", mode.Perm())
}

// resolvePath joins basePath with a resource's relative path and ensures the
// result stays within basePath (rejects absolute paths and `..` traversal).
func resolvePath(basePath, relative string) (string, error) {
	if relative == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("path must be relative: %q", relative)
	}
	clean := filepath.Clean(relative)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes basePath: %q", relative)
	}
	return filepath.Join(basePath, clean), nil
}

func (p *Plugin) RateLimit() pkgmodel.RateLimitConfig {
	return pkgmodel.RateLimitConfig{Scope: pkgmodel.RateLimitScopeNamespace, MaxRequestsPerSecondForNamespace: 10}
}
func (p *Plugin) LabelConfig() pkgmodel.LabelConfig        { return pkgmodel.LabelConfig{DefaultQuery: "$.path"} }
func (p *Plugin) DiscoveryFilters() []pkgmodel.MatchFilter { return nil }

func (p *Plugin) Create(ctx context.Context, req *resource.CreateRequest) (*resource.CreateResult, error) {
	if req.ResourceType != ResourceTypeDirectory {
		return createFailure(resource.OperationErrorCodeInvalidRequest, "unsupported resource type: "+req.ResourceType), nil
	}
	cfg, err := parseTargetConfig(req.TargetConfig)
	if err != nil {
		return createFailure(resource.OperationErrorCodeInvalidRequest, err.Error()), nil
	}
	var props DirectoryProperties
	if err := json.Unmarshal(req.Properties, &props); err != nil {
		return createFailure(resource.OperationErrorCodeInvalidRequest, "invalid properties: "+err.Error()), nil
	}
	full, err := resolvePath(cfg.BasePath, props.Path)
	if err != nil {
		return createFailure(resource.OperationErrorCodeInvalidRequest, err.Error()), nil
	}
	if props.Permissions == "" {
		props.Permissions = defaultDirPerm
	}
	mode, err := parsePermissions(props.Permissions)
	if err != nil {
		return createFailure(resource.OperationErrorCodeInvalidRequest, err.Error()), nil
	}
	if _, err := os.Stat(full); err == nil {
		return createFailure(resource.OperationErrorCodeAlreadyExists, "directory already exists: "+props.Path), nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return createFailure(resource.OperationErrorCodeInternalFailure, err.Error()), nil
	}
	if err := os.MkdirAll(full, mode); err != nil {
		return createFailure(resource.OperationErrorCodeInternalFailure, err.Error()), nil
	}
	if err := os.Chmod(full, mode); err != nil {
		return createFailure(resource.OperationErrorCodeInternalFailure, err.Error()), nil
	}
	propsJSON, _ := json.Marshal(props)
	return &resource.CreateResult{ProgressResult: &resource.ProgressResult{
		Operation:          resource.OperationCreate,
		OperationStatus:    resource.OperationStatusSuccess,
		NativeID:           props.Path,
		ResourceProperties: propsJSON,
	}}, nil
}

func (p *Plugin) Read(ctx context.Context, req *resource.ReadRequest) (*resource.ReadResult, error) {
	if req.ResourceType != ResourceTypeDirectory {
		return &resource.ReadResult{ResourceType: req.ResourceType, ErrorCode: resource.OperationErrorCodeInvalidRequest}, nil
	}
	cfg, err := parseTargetConfig(req.TargetConfig)
	if err != nil {
		return &resource.ReadResult{ResourceType: req.ResourceType, ErrorCode: resource.OperationErrorCodeInvalidRequest}, nil
	}
	full, err := resolvePath(cfg.BasePath, req.NativeID)
	if err != nil {
		return &resource.ReadResult{ResourceType: req.ResourceType, ErrorCode: resource.OperationErrorCodeInvalidRequest}, nil
	}
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &resource.ReadResult{ResourceType: req.ResourceType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: req.ResourceType, ErrorCode: resource.OperationErrorCodeInternalFailure}, nil
	}
	if !info.IsDir() {
		return &resource.ReadResult{ResourceType: req.ResourceType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
	}
	propsJSON, _ := json.Marshal(DirectoryProperties{
		Path:        req.NativeID,
		Permissions: formatPerm(info.Mode()),
	})
	return &resource.ReadResult{ResourceType: req.ResourceType, Properties: string(propsJSON)}, nil
}

func (p *Plugin) Update(ctx context.Context, req *resource.UpdateRequest) (*resource.UpdateResult, error) {
	if req.ResourceType != ResourceTypeDirectory {
		return updateFailure(resource.OperationErrorCodeInvalidRequest, "unsupported resource type: "+req.ResourceType), nil
	}
	cfg, err := parseTargetConfig(req.TargetConfig)
	if err != nil {
		return updateFailure(resource.OperationErrorCodeInvalidRequest, err.Error()), nil
	}
	full, err := resolvePath(cfg.BasePath, req.NativeID)
	if err != nil {
		return updateFailure(resource.OperationErrorCodeInvalidRequest, err.Error()), nil
	}
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return updateFailure(resource.OperationErrorCodeNotFound, "directory not found: "+req.NativeID), nil
		}
		return updateFailure(resource.OperationErrorCodeInternalFailure, err.Error()), nil
	}
	if !info.IsDir() {
		return updateFailure(resource.OperationErrorCodeInvalidRequest, "expected directory at "+req.NativeID+", found file"), nil
	}
	var prior, desired DirectoryProperties
	if err := json.Unmarshal(req.PriorProperties, &prior); err != nil {
		return updateFailure(resource.OperationErrorCodeInvalidRequest, "invalid prior properties: "+err.Error()), nil
	}
	if err := json.Unmarshal(req.DesiredProperties, &desired); err != nil {
		return updateFailure(resource.OperationErrorCodeInvalidRequest, "invalid desired properties: "+err.Error()), nil
	}
	if desired.Permissions == "" {
		desired.Permissions = defaultDirPerm
	}
	if prior.Permissions != desired.Permissions {
		mode, err := parsePermissions(desired.Permissions)
		if err != nil {
			return updateFailure(resource.OperationErrorCodeInvalidRequest, err.Error()), nil
		}
		if err := os.Chmod(full, mode); err != nil {
			return updateFailure(resource.OperationErrorCodeInternalFailure, err.Error()), nil
		}
	}
	propsJSON, _ := json.Marshal(desired)
	return &resource.UpdateResult{ProgressResult: &resource.ProgressResult{
		Operation:          resource.OperationUpdate,
		OperationStatus:    resource.OperationStatusSuccess,
		NativeID:           req.NativeID,
		ResourceProperties: propsJSON,
	}}, nil
}

func (p *Plugin) Delete(ctx context.Context, req *resource.DeleteRequest) (*resource.DeleteResult, error) {
	if req.ResourceType != ResourceTypeDirectory {
		return deleteFailure(resource.OperationErrorCodeInvalidRequest, "unsupported resource type: "+req.ResourceType), nil
	}
	cfg, err := parseTargetConfig(req.TargetConfig)
	if err != nil {
		return deleteFailure(resource.OperationErrorCodeInvalidRequest, err.Error()), nil
	}
	full, err := resolvePath(cfg.BasePath, req.NativeID)
	if err != nil {
		return deleteFailure(resource.OperationErrorCodeInvalidRequest, err.Error()), nil
	}
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &resource.DeleteResult{ProgressResult: &resource.ProgressResult{
				Operation:       resource.OperationDelete,
				OperationStatus: resource.OperationStatusFailure,
				ErrorCode:       resource.OperationErrorCodeNotFound,
				NativeID:        req.NativeID,
			}}, nil
		}
		return deleteFailure(resource.OperationErrorCodeInternalFailure, err.Error()), nil
	}
	if !info.IsDir() {
		return deleteFailure(resource.OperationErrorCodeInvalidRequest, "expected directory at "+req.NativeID+", found file"), nil
	}
	if err := os.Remove(full); err != nil {
		return deleteFailure(resource.OperationErrorCodeInternalFailure, err.Error()), nil
	}
	return &resource.DeleteResult{ProgressResult: &resource.ProgressResult{
		Operation:       resource.OperationDelete,
		OperationStatus: resource.OperationStatusSuccess,
		NativeID:        req.NativeID,
	}}, nil
}

func (p *Plugin) Status(ctx context.Context, req *resource.StatusRequest) (*resource.StatusResult, error) {
	return &resource.StatusResult{ProgressResult: &resource.ProgressResult{
		Operation:       resource.OperationCheckStatus,
		OperationStatus: resource.OperationStatusFailure,
		ErrorCode:       resource.OperationErrorCodeInvalidRequest,
		StatusMessage:   "synchronous plugin: no async operations to query",
	}}, nil
}

func (p *Plugin) List(ctx context.Context, req *resource.ListRequest) (*resource.ListResult, error) {
	ids := []string{}
	if req.ResourceType != ResourceTypeDirectory {
		return &resource.ListResult{NativeIDs: ids}, nil
	}
	cfg, err := parseTargetConfig(req.TargetConfig)
	if err != nil {
		return &resource.ListResult{NativeIDs: ids}, nil
	}
	if _, err := os.Stat(cfg.BasePath); err != nil {
		return &resource.ListResult{NativeIDs: ids}, nil
	}
	walkErr := filepath.WalkDir(cfg.BasePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == cfg.BasePath || !d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(cfg.BasePath, path)
		if relErr != nil {
			return relErr
		}
		ids = append(ids, rel)
		return nil
	})
	if walkErr != nil {
		return &resource.ListResult{NativeIDs: []string{}}, nil
	}
	return &resource.ListResult{NativeIDs: ids}, nil
}

func createFailure(code resource.OperationErrorCode, msg string) *resource.CreateResult {
	return &resource.CreateResult{ProgressResult: &resource.ProgressResult{
		Operation: resource.OperationCreate, OperationStatus: resource.OperationStatusFailure, ErrorCode: code, StatusMessage: msg,
	}}
}
func updateFailure(code resource.OperationErrorCode, msg string) *resource.UpdateResult {
	return &resource.UpdateResult{ProgressResult: &resource.ProgressResult{
		Operation: resource.OperationUpdate, OperationStatus: resource.OperationStatusFailure, ErrorCode: code, StatusMessage: msg,
	}}
}
func deleteFailure(code resource.OperationErrorCode, msg string) *resource.DeleteResult {
	return &resource.DeleteResult{ProgressResult: &resource.ProgressResult{
		Operation: resource.OperationDelete, OperationStatus: resource.OperationStatusFailure, ErrorCode: code, StatusMessage: msg,
	}}
}
