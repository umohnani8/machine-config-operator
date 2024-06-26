//go:build !ignore_autogenerated
// +build !ignore_autogenerated

// Code generated by deepcopy-gen. DO NOT EDIT.

package v1alpha1

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MCOObjectReference) DeepCopyInto(out *MCOObjectReference) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MCOObjectReference.
func (in *MCOObjectReference) DeepCopy() *MCOObjectReference {
	if in == nil {
		return nil
	}
	out := new(MCOObjectReference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MachineConfigNode) DeepCopyInto(out *MachineConfigNode) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	out.Spec = in.Spec
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MachineConfigNode.
func (in *MachineConfigNode) DeepCopy() *MachineConfigNode {
	if in == nil {
		return nil
	}
	out := new(MachineConfigNode)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MachineConfigNode) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MachineConfigNodeList) DeepCopyInto(out *MachineConfigNodeList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]MachineConfigNode, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MachineConfigNodeList.
func (in *MachineConfigNodeList) DeepCopy() *MachineConfigNodeList {
	if in == nil {
		return nil
	}
	out := new(MachineConfigNodeList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *MachineConfigNodeList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MachineConfigNodeSpec) DeepCopyInto(out *MachineConfigNodeSpec) {
	*out = *in
	out.Node = in.Node
	out.Pool = in.Pool
	out.ConfigVersion = in.ConfigVersion
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MachineConfigNodeSpec.
func (in *MachineConfigNodeSpec) DeepCopy() *MachineConfigNodeSpec {
	if in == nil {
		return nil
	}
	out := new(MachineConfigNodeSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MachineConfigNodeSpecMachineConfigVersion) DeepCopyInto(out *MachineConfigNodeSpecMachineConfigVersion) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MachineConfigNodeSpecMachineConfigVersion.
func (in *MachineConfigNodeSpecMachineConfigVersion) DeepCopy() *MachineConfigNodeSpecMachineConfigVersion {
	if in == nil {
		return nil
	}
	out := new(MachineConfigNodeSpecMachineConfigVersion)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MachineConfigNodeStatus) DeepCopyInto(out *MachineConfigNodeStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]v1.Condition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	out.ConfigVersion = in.ConfigVersion
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MachineConfigNodeStatus.
func (in *MachineConfigNodeStatus) DeepCopy() *MachineConfigNodeStatus {
	if in == nil {
		return nil
	}
	out := new(MachineConfigNodeStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *MachineConfigNodeStatusMachineConfigVersion) DeepCopyInto(out *MachineConfigNodeStatusMachineConfigVersion) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new MachineConfigNodeStatusMachineConfigVersion.
func (in *MachineConfigNodeStatusMachineConfigVersion) DeepCopy() *MachineConfigNodeStatusMachineConfigVersion {
	if in == nil {
		return nil
	}
	out := new(MachineConfigNodeStatusMachineConfigVersion)
	in.DeepCopyInto(out)
	return out
}
