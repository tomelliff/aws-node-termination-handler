// Copyright 2022 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//     http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package asglifecycletargetstate

import (
	"crypto/sha256"
	"fmt"
	"github.com/aws/aws-node-termination-handler/pkg/ec2metadata"
	"github.com/aws/aws-node-termination-handler/pkg/monitor"
	"github.com/aws/aws-node-termination-handler/pkg/node"
	"time"
)

const (
	// ASGLifeCycleTargetStateKind is a const to define an Autoscaling Group Lifecycle Target State kind of event
	ASGLifeCycleTargetStateKind = "AUTO_SCALING_GROUP_TERMINATION_HOOK"
)

type ASGLifeCycleTargetStateMonitor struct {
	IMDS             *ec2metadata.Service
	InterruptionChan chan<- monitor.InterruptionEvent
	NodeName         string
}

func NewASGLifeCycleTargetStateMonitor(IMDS *ec2metadata.Service, interruptionChan chan<- monitor.InterruptionEvent, nodeName string) *ASGLifeCycleTargetStateMonitor {
	return &ASGLifeCycleTargetStateMonitor{IMDS: IMDS, InterruptionChan: interruptionChan, NodeName: nodeName}
}

func (m ASGLifeCycleTargetStateMonitor) Monitor() error {
	interruptionEvent, err := m.checkForASGLifecycleTargetStateTerminated()
	if err != nil {
		return err
	}
	if interruptionEvent != nil && interruptionEvent.Kind == ASGLifeCycleTargetStateKind {
		m.InterruptionChan <- *interruptionEvent
	}
	return nil
}

func (m ASGLifeCycleTargetStateMonitor) Kind() string {
	return ASGLifeCycleTargetStateKind
}

func (m ASGLifeCycleTargetStateMonitor) checkForASGLifecycleTargetStateTerminated() (*monitor.InterruptionEvent, error) {
	asgLifecycleTargetState, err := m.IMDS.GetAutoScalingGroupLifecycleTargetStatusEvent()
	if err != nil {
		return nil, fmt.Errorf("There was a problem checking for ASG lifecycle target state: %w", err)
	}
	// If the target state isn't 'Terminated' then we don't need to do anything
	if asgLifecycleTargetState != "Terminated" {
		return nil, nil
	}
	// There's no EventID returned so we'll create it using a hash to prevent duplicates.
	// TODO: Check whether this is valid as we don't have anything uniqueish here like a timestamp
	//		 What does the hash get used for when not using queue processor mode?
	hash := sha256.New()
	_, err = hash.Write([]byte(asgLifecycleTargetState))
	if err != nil {
		return nil, fmt.Errorf("There was a problem creating an event ID from the event: %w", err)
	}

	return &monitor.InterruptionEvent{
		EventID:      fmt.Sprintf("asg-lifecycle-target-state-%x", hash.Sum(nil)),
		Kind:         ASGLifeCycleTargetStateKind,
		Description:  "Instance is being terminated and will be cordoned now",
		NodeName:     m.NodeName,
		StartTime:    time.Now(),
		PreDrainTask: setInterruptionTaint,
	}, nil
}

func setInterruptionTaint(interruptionEvent monitor.InterruptionEvent, n node.Node) error {
	err := n.TaintASGLifecycleTermination(interruptionEvent.NodeName, interruptionEvent.EventID)
	if err != nil {
		return fmt.Errorf("Unable to taint node with taint %s:%s: %w", node.ASGLifecycleTerminationTaint, interruptionEvent.EventID, err)
	}

	return nil
}
