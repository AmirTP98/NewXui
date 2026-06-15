package controller

import (
	"context"
	"strconv"
	"time"

	"github.com/alireza0/x-ui/database/model"
	"github.com/alireza0/x-ui/web/service"

	"github.com/gin-gonic/gin"
)

// NodeController exposes the Multi Nodes API under /xui/API/nodes.
type NodeController struct {
	nodeService   service.NodeService
	bridgeService service.NodeBridgeService
}

func (a *NodeController) getNodes(c *gin.Context) {
	nodes, err := a.nodeService.GetAllNodes()
	jsonObj(c, nodes, err)
}

func (a *NodeController) getNode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "get", err)
		return
	}
	node, err := a.nodeService.GetNode(id)
	jsonObj(c, node, err)
}

func (a *NodeController) addNode(c *gin.Context) {
	node := &model.Node{}
	if err := c.ShouldBind(node); err != nil {
		jsonMsg(c, "add node", err)
		return
	}
	node.Id = 0
	err := a.nodeService.AddNode(node)
	jsonMsgObj(c, "add node", node, err)
}

func (a *NodeController) updateNode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "update node", err)
		return
	}
	node, err := a.nodeService.GetNode(id)
	if err != nil {
		jsonMsg(c, "update node", err)
		return
	}
	if err := c.ShouldBind(node); err != nil {
		jsonMsg(c, "update node", err)
		return
	}
	node.Id = id
	err = a.nodeService.UpdateNode(node)
	jsonMsgObj(c, "update node", node, err)
}

func (a *NodeController) delNode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "delete node", err)
		return
	}
	err = a.nodeService.DeleteNode(id)
	jsonMsg(c, "delete node", err)
}

func (a *NodeController) testNode(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "test node", err)
		return
	}
	node, err := a.nodeService.GetNode(id)
	if err != nil {
		jsonMsg(c, "test node", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	testErr := a.nodeService.TestConnection(ctx, node)
	a.nodeService.UpdateNodeStatus(id, testErr)
	jsonMsg(c, "test node", testErr)
}

func (a *NodeController) getOutboundTags(c *gin.Context) {
	tags, err := a.bridgeService.GetOutboundTags()
	jsonObj(c, tags, err)
}

func (a *NodeController) getMasterConfig(c *gin.Context) {
	config, err := a.nodeService.GetMasterConfig()
	jsonObj(c, config, err)
}

func (a *NodeController) setMaster(c *gin.Context) {
	id, err := strconv.Atoi(c.PostForm("inboundId"))
	if err != nil {
		jsonMsg(c, "set master inbound", err)
		return
	}
	err = a.nodeService.SetMasterInboundId(id)
	jsonMsg(c, "set master inbound", err)
}

func (a *NodeController) createNodeInbound(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		jsonMsg(c, "create node inbound", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	err = a.nodeService.CreateNodeInbound(ctx, id)
	jsonMsg(c, "create node inbound", err)
}

func (a *NodeController) getTrafficSyncInterval(c *gin.Context) {
	interval, err := a.nodeService.GetTrafficSyncInterval()
	jsonObj(c, interval, err)
}

func (a *NodeController) setTrafficSyncInterval(c *gin.Context) {
	seconds, err := strconv.Atoi(c.PostForm("interval"))
	if err != nil {
		jsonMsg(c, "set traffic sync interval", err)
		return
	}
	err = a.nodeService.SetTrafficSyncInterval(seconds)
	jsonMsg(c, "set traffic sync interval", err)
}
