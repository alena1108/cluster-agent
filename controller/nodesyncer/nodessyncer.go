package nodesyncer

import (
	"fmt"

	clusterv1 "github.com/rancher/types/apis/cluster.cattle.io/v1"
	"github.com/rancher/types/config"
	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NodeSyncer struct {
	ClusterNodes clusterv1.ClusterNodeInterface
	Clusters     clusterv1.ClusterInterface
	clusterName  string
}

func Register(workload *config.WorkloadContext) {
	n := &NodeSyncer{
		clusterName:  workload.ClusterName,
		ClusterNodes: workload.Cluster.Cluster.ClusterNodes(""),
		Clusters:     workload.Cluster.Cluster.Clusters(""),
	}

	workload.Core.Nodes("").Controller().AddHandler(n.sync)
}

func (n *NodeSyncer) sync(key string, node *v1.Node) error {
	if node == nil {
		return n.deleteClusterNode(key)
	}
	return n.createOrUpdateClusterNode(node)
}

func (n *NodeSyncer) deleteClusterNode(nodeName string) error {
	clusterNode, err := n.getClusterNode(nodeName)
	if err != nil {
		return err
	}
	logrus.Infof("Deleting cluster node [%s]", nodeName)

	if clusterNode == nil {
		logrus.Infof("ClusterNode [%s] is already deleted")
		return nil
	}
	err = n.ClusterNodes.Delete(clusterNode.ObjectMeta.Name, nil)
	if err != nil {
		return fmt.Errorf("Failed to delete cluster node [%s] %v", nodeName, err)
	}
	logrus.Infof("Deleted cluster node [%s]", nodeName)
	return nil
}

func (n *NodeSyncer) getClusterNode(nodeName string) (*clusterv1.ClusterNode, error) {
	nodes, err := n.ClusterNodes.List(metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	for _, node := range nodes.Items {
		if node.NodeName == nodeName {
			return &node, nil
		}
	}

	return nil, nil
}

func (n *NodeSyncer) createOrUpdateClusterNode(node *v1.Node) error {
	existing, err := n.getClusterNode(node.Name)
	if err != nil {
		return err
	}
	cluster, err := n.Clusters.Get(n.clusterName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("Failed to get cluster [%s] %v", n.clusterName, err)
	}
	clusterNode := n.convertNodeToClusterNode(node, cluster)

	if cluster.ObjectMeta.DeletionTimestamp != nil {
		return fmt.Errorf("Cluster [%s] in removing state", n.clusterName)
	}
	if existing == nil {
		logrus.Infof("Creating cluster node [%s]", node.Name)
		clusterNode.Status.Requested = make(map[v1.ResourceName]resource.Quantity)
		clusterNode.Status.Limits = make(map[v1.ResourceName]resource.Quantity)
		_, err := n.ClusterNodes.Create(clusterNode)
		if err != nil {
			return fmt.Errorf("Failed to create cluster node [%s] %v", node.Name, err)
		}
		logrus.Infof("Created cluster node [%s]", node.Name)
	} else {
		logrus.Infof("Updating cluster node [%s]", node.Name)
		//TODO - consider doing merge2ways once more than one controller modifies the clusterNode
		clusterNode.ObjectMeta.ResourceVersion = existing.ObjectMeta.ResourceVersion
		clusterNode.Name = existing.Name
		clusterNode.Status.Requested = existing.Status.Requested
		clusterNode.Status.Limits = existing.Status.Limits
		_, err := n.ClusterNodes.Update(clusterNode)
		if err != nil {
			return fmt.Errorf("Failed to update cluster node [%s] %v", node.Name, err)
		}
		logrus.Infof("Updated cluster node [%s]", node.Name)
	}
	return nil
}

func (n *NodeSyncer) convertNodeToClusterNode(node *v1.Node, cluster *clusterv1.Cluster) *clusterv1.ClusterNode {
	if node == nil {
		return nil
	}
	clusterNode := &clusterv1.ClusterNode{
		NodeSpec: node.Spec,
		Status: clusterv1.ClusterNodeStatus{
			NodeStatus: node.Status,
		},
	}
	clusterNode.APIVersion = "cluster.cattle.io/v1"
	clusterNode.Kind = "ClusterNode"
	clusterNode.ClusterName = n.clusterName
	clusterNode.NodeName = node.Name
	clusterNode.ObjectMeta = metav1.ObjectMeta{
		GenerateName: "clusternode-",
		Labels:       node.Labels,
		Annotations:  node.Annotations,
	}
	ref := metav1.OwnerReference{
		Name:       n.clusterName,
		UID:        cluster.UID,
		APIVersion: cluster.APIVersion,
		Kind:       cluster.Kind,
	}
	clusterNode.ObjectMeta.OwnerReferences = append(clusterNode.ObjectMeta.OwnerReferences, ref)
	return clusterNode
}
