package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ethpandaops/dora/services"
	"github.com/ethpandaops/dora/templates"
	"github.com/ethpandaops/dora/types/models"
	"github.com/ethpandaops/dora/utils"
	"github.com/sirupsen/logrus"
)

// Clients will return the main "clients" page using a go template
func Clients(w http.ResponseWriter, r *http.Request) {
	var clientsTemplateFiles = append(layoutTemplateFiles,
		"clients/clients.html",
	)

	var pageTemplate = templates.GetTemplate(clientsTemplateFiles...)
	data := InitPageData(w, r, "clients", "/clients", "Clients", clientsTemplateFiles)

	var pageError error
	pageError = services.GlobalCallRateLimiter.CheckCallLimit(r, 1)
	if pageError == nil {
		data.Data, pageError = getClientsPageData()
	}
	if pageError != nil {
		handlePageError(w, r, pageError)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	if handleTemplateError(w, r, "clients.go", "Clients", "", pageTemplate.ExecuteTemplate(w, "layout", data)) != nil {
		return // an error has occurred and was processed
	}
}

func getClientsPageData() (*models.ClientsPageData, error) {
	pageData := &models.ClientsPageData{}
	pageCacheKey := "clients"
	pageRes, pageErr := services.GlobalFrontendCache.ProcessCachedPage(pageCacheKey, true, pageData, func(pageCall *services.FrontendCacheProcessingPage) interface{} {
		pageData, cacheTimeout := buildClientsPageData()
		pageCall.CacheTimeout = cacheTimeout
		return pageData
	})
	if pageErr == nil && pageRes != nil {
		resData, resOk := pageRes.(*models.ClientsPageData)
		if !resOk {
			return nil, ErrInvalidPageModel
		}
		pageData = resData
	}
	return pageData, pageErr
}

func buildPeerMapData() *models.ClientPageDataPeerMap {
	peerMap := &models.ClientPageDataPeerMap{
		ClientPageDataMapNode: []*models.ClientPageDataPeerMapNode{},
		ClientDataMapEdges:    []*models.ClientDataMapPeerMapEdge{},
	}

	nodes := make(map[string]*models.ClientPageDataPeerMapNode)
	edges := make(map[string]*models.ClientDataMapPeerMapEdge)

	for _, client := range services.GlobalBeaconService.GetClients() {
		peerId := client.GetPeerId()
		if _, ok := nodes[peerId]; !ok {
			node := models.ClientPageDataPeerMapNode{
				Id:    peerId,
				Label: client.GetName(),
				Group: "internal",
				Image: fmt.Sprintf("https://api.dicebear.com/9.x/identicon/svg?seed=%s", peerId),
				Shape: "circularImage",
			}
			nodes[peerId] = &node
			peerMap.ClientPageDataMapNode = append(peerMap.ClientPageDataMapNode, &node)
		}
	}

	for _, client := range services.GlobalBeaconService.GetClients() {
		peerId := client.GetPeerId()
		peers := client.GetNodePeers()
		for _, peer := range peers {
			peerId := peerId
			// Check if the PeerId is already in the nodes map, if not add it as an "external" node
			if _, ok := nodes[peer.PeerID]; !ok {
				node := models.ClientPageDataPeerMapNode{
					Id:    peer.PeerID,
					Label: fmt.Sprintf("%s...%s", peer.PeerID[0:5], peer.PeerID[len(peer.PeerID)-5:]),
					Group: "external",
					Image: fmt.Sprintf("https://api.dicebear.com/9.x/identicon/svg?seed=%s", peer.PeerID),
					Shape: "circularImage",
				}
				nodes[peer.PeerID] = &node
				peerMap.ClientPageDataMapNode = append(peerMap.ClientPageDataMapNode, &node)
			}

			// Deduplicate edges. When adding an edge, we index by sorted peer IDs.
			sortedPeerIds := []string{peerId, peer.PeerID}
			sort.Strings(sortedPeerIds)
			idx := strings.Join(sortedPeerIds, "-")

			// Increase value based on peer count
			p1 := nodes[peer.PeerID]
			p1.Value++
			nodes[peer.PeerID] = p1
			p2 := nodes[peerId]
			p2.Value++

			if _, ok := edges[idx]; !ok {
				edge := models.ClientDataMapPeerMapEdge{}
				if nodes[peer.PeerID].Group == "external" {
					edge.Dashes = true
				}
				if peer.Direction == "inbound" {
					edge.From = peer.PeerID
					edge.To = peerId
				} else {
					edge.From = peerId
					edge.To = peer.PeerID
				}
				edges[idx] = &edge
				peerMap.ClientDataMapEdges = append(peerMap.ClientDataMapEdges, &edge)
			}
		}
	}

	return peerMap
}

func buildClientsPageData() (*models.ClientsPageData, time.Duration) {
	logrus.Debugf("clients page called")
	pageData := &models.ClientsPageData{
		Clients: []*models.ClientsPageDataClient{},
		PeerMap: buildPeerMapData(),
	}
	cacheTime := time.Duration(utils.Config.Chain.Config.SecondsPerSlot) * time.Second

	for _, client := range services.GlobalBeaconService.GetClients() {
		lastHeadSlot, lastHeadRoot, clientRefresh := client.GetLastHead()
		if lastHeadSlot < 0 {
			lastHeadSlot = 0
		}
		resClient := &models.ClientsPageDataClient{
			Index:       int(client.GetIndex()) + 1,
			Name:        client.GetName(),
			Version:     client.GetVersion(),
			Peers:       client.GetNodePeers(),
			PeerId:      client.GetPeerId(),
			HeadSlot:    uint64(lastHeadSlot),
			HeadRoot:    lastHeadRoot,
			Status:      client.GetStatus(),
			LastRefresh: clientRefresh,
			LastError:   client.GetLastClientError(),
		}
		pageData.Clients = append(pageData.Clients, resClient)

	}
	pageData.ClientCount = uint64(len(pageData.Clients))

	return pageData, cacheTime
}
