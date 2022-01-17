package network

//go:generate sh -c "mockgen -package network -destination mock_resource_manager.go github.com/libp2p/go-libp2p-core/network ResourceManager"
//go:generate sh -c "mockgen -package network -destination mock_conn_management_scope.go github.com/libp2p/go-libp2p-core/network ConnManagementScope"
//go:generate sh -c "mockgen -package network -destination mock_stream_management_scope.go github.com/libp2p/go-libp2p-core/network StreamManagementScope"
//go:generate sh -c "mockgen -package network -destination mock_peer_scope.go github.com/libp2p/go-libp2p-core/network PeerScope"
//go:generate sh -c "mockgen -package network -destination mock_protocol_scope.go github.com/libp2p/go-libp2p-core/network ProtocolScope"
