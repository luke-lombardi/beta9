package types

import (
	"time"
)

const (
	DefaultExtractedObjectPath         string        = "/data/unpacked"
	DefaultVolumesPath                 string        = "/data/volumes"
	DefaultObjectPath                  string        = "/data/objects"
	DefaultFilesystemName              string        = "beta9-fs"
	DefaultFilesystemPath              string        = "/data"
	FailedDeploymentContainerThreshold int           = 3
	FailedContainerThreshold           int           = 1
	RequestTimeoutDurationS            time.Duration = 175 * time.Second
	ContainerVolumePath                string        = "/volumes"
)

type ContainerEvent struct {
	ContainerId string
	Change      int
}

type AvailableHost struct {
	Hostname    string
	ContainerId string
}
