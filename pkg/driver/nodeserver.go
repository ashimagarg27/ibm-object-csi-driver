/*******************************************************************************
 * IBM Confidential
 * OCO Source Materials
 * IBM Cloud Container Service, 5737-D43
 * (C) Copyright IBM Corp. 2019, 2020 All Rights Reserved.
 * The source code for this program is not  published or otherwise divested of
 * its trade secrets, irrespective of what has been deposited with
 * the U.S. Copyright Office.
 ******************************************************************************/
package driver

import (
	"fmt"
	"github.com/IBM/satellite-object-storage-plugin/pkg/mounter"
	"github.com/container-storage-interface/spec/lib/go/csi"
	csicommon "github.com/kubernetes-csi/drivers/pkg/csi-common"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/util/mount"
	"os"
	"sync"
)

type nodeServer struct {
	*csicommon.DefaultNodeServer
	mux sync.Mutex
	*s3Driver
}

var (
	mounterObj      mounter.Mounter
	newmounter      = mounter.NewMounter
	fuseunmount     = mounter.FuseUnmount
	checkMountpoint = checkMount
)

func (ns *nodeServer) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	var (
		val       string
		check     bool
		accessKey string
		secretKey string
	)

	klog.Infof("CSINodeServer-NodePublishVolume...| Request %v", *req)

	ns.mux.Lock()
	defer ns.mux.Unlock()

	volumeID := req.GetVolumeId()

	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	targetPath := req.GetTargetPath()
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	stagingTargetPath := req.GetStagingTargetPath()

	if len(stagingTargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Staging Target path missing in request")
	}

	// Check arguments
	if req.GetVolumeCapability() == nil {
		return nil, status.Error(codes.InvalidArgument, "Volume capability missing in request")
	}

	notMnt, err := checkMountpoint(targetPath)
	if err != nil {
		klog.Errorf("Can not validate target mount point: %s %v", targetPath, err)
		return nil, status.Error(codes.Internal, err.Error())
	}
	if !notMnt {
		return &csi.NodePublishVolumeResponse{}, nil
	}

	deviceID := ""
	if req.GetPublishContext() != nil {
		deviceID = req.GetPublishContext()[deviceID]
	}

	readOnly := req.GetReadonly()
	attrib := req.GetVolumeContext()
	mountFlags := req.GetVolumeCapability().GetMount().GetMountFlags()

	klog.Infof("target %v\ndevice %v\nreadonly %v\nvolumeId %v\nattributes %v\nmountflags %v\n",
		targetPath, deviceID, readOnly, volumeID, attrib, mountFlags)

	secretMap := req.GetSecrets()
	if val, check = secretMap["access-key"]; check {
		accessKey = val
	}
	if val, check = secretMap["secret-key"]; check {
		secretKey = val
	}
	fmt.Println("CreateVolume VolumeContext:\n\t", attrib)
	fmt.Println("CreateVolume Secrets:\n\t", secretMap)

	//func newMounter(mounter string, bucket string, objpath string, endpoint string, region string, keys string) (Mounter, error) {
	if mounterObj, err = newmounter("s3fs",
		attrib["bucket-name"], attrib["obj-path"],
		attrib["cos-endpoint"], attrib["regn-class"],
		fmt.Sprintf("%s:%s", accessKey, secretKey)); err != nil {
		return nil, err
	}

	klog.Info("-NodePublishVolume-: Mount")

	if err = mounterObj.Mount("", targetPath); err != nil {
		return nil, err
	}

	klog.Infof("s3: bucket %s successfuly mounted to %s", attrib["bucket-name"], targetPath)
	return &csi.NodePublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	klog.Infof("CSINodeServer-NodeUnpublishVolume...| Request: %v", *req)
	ns.mux.Lock()
	defer ns.mux.Unlock()

	volumeID := req.GetVolumeId()
	targetPath := req.GetTargetPath()

	// Check arguments
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(targetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}
	klog.Infof("Unmounting  target path %s", targetPath)

	if err := fuseunmount(targetPath); err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	klog.Infof("Successfully unmounted  target path %s", targetPath)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (ns *nodeServer) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	klog.Infof("CSINodeServer-NodeStageVolume... | Request %v", *req)

	ns.mux.Lock()
	defer ns.mux.Unlock()

	volumeID := req.GetVolumeId()
	stagingTargetPath := req.GetStagingTargetPath()

	// Check arguments
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	if len(stagingTargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}

	if req.VolumeCapability == nil {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume Volume Capability must be provided")
	}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (ns *nodeServer) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	klog.V(2).Infof("CSINodeServer-NodeUnstageVolume ... | Request %v", *req)
	ns.mux.Lock()
	defer ns.mux.Unlock()

	volumeID := req.GetVolumeId()
	stagingTargetPath := req.GetStagingTargetPath()

	// Check arguments
	if len(volumeID) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	if len(stagingTargetPath) == 0 {
		return nil, status.Error(codes.InvalidArgument, "Target path missing in request")
	}
	klog.Infof("Unmounting staging target path %s", stagingTargetPath)
	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodeGetCapabilities returns the supported capabilities of the node server
func (ns *nodeServer) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	// currently there is a single NodeServer capability according to the spec
	nscap := &csi.NodeServiceCapability{
		Type: &csi.NodeServiceCapability_Rpc{
			Rpc: &csi.NodeServiceCapability_RPC{
				Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
			},
		},
	}

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			nscap,
		},
	}, nil
}

func (ns *nodeServer) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return &csi.NodeExpandVolumeResponse{}, status.Error(codes.Unimplemented, "NodeExpandVolume is not implemented")
}

func checkMount(targetPath string) (bool, error) {
	notMnt, err := mount.New("").IsLikelyNotMountPoint(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err = os.MkdirAll(targetPath, 0750); err != nil {
				return false, err
			}
			notMnt = true
		} else {
			return false, err
		}
	}
	return notMnt, nil
}