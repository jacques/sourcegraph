// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.3.0
// - protoc             (unknown)
// source: searcher.proto

package v1

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

const (
	SearcherService_Search_FullMethodName = "/searcher.v1.SearcherService/Search"
)

// SearcherServiceClient is the client API for SearcherService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type SearcherServiceClient interface {
	// Search executes a search, streaming back its results
	Search(ctx context.Context, in *SearchRequest, opts ...grpc.CallOption) (SearcherService_SearchClient, error)
}

type searcherServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewSearcherServiceClient(cc grpc.ClientConnInterface) SearcherServiceClient {
	return &searcherServiceClient{cc}
}

func (c *searcherServiceClient) Search(ctx context.Context, in *SearchRequest, opts ...grpc.CallOption) (SearcherService_SearchClient, error) {
	stream, err := c.cc.NewStream(ctx, &SearcherService_ServiceDesc.Streams[0], SearcherService_Search_FullMethodName, opts...)
	if err != nil {
		return nil, err
	}
	x := &searcherServiceSearchClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type SearcherService_SearchClient interface {
	Recv() (*SearchResponse, error)
	grpc.ClientStream
}

type searcherServiceSearchClient struct {
	grpc.ClientStream
}

func (x *searcherServiceSearchClient) Recv() (*SearchResponse, error) {
	m := new(SearchResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// SearcherServiceServer is the server API for SearcherService service.
// All implementations must embed UnimplementedSearcherServiceServer
// for forward compatibility
type SearcherServiceServer interface {
	// Search executes a search, streaming back its results
	Search(*SearchRequest, SearcherService_SearchServer) error
	mustEmbedUnimplementedSearcherServiceServer()
}

// UnimplementedSearcherServiceServer must be embedded to have forward compatible implementations.
type UnimplementedSearcherServiceServer struct {
}

func (UnimplementedSearcherServiceServer) Search(*SearchRequest, SearcherService_SearchServer) error {
	return status.Errorf(codes.Unimplemented, "method Search not implemented")
}
func (UnimplementedSearcherServiceServer) mustEmbedUnimplementedSearcherServiceServer() {}

// UnsafeSearcherServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to SearcherServiceServer will
// result in compilation errors.
type UnsafeSearcherServiceServer interface {
	mustEmbedUnimplementedSearcherServiceServer()
}

func RegisterSearcherServiceServer(s grpc.ServiceRegistrar, srv SearcherServiceServer) {
	s.RegisterService(&SearcherService_ServiceDesc, srv)
}

func _SearcherService_Search_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(SearchRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(SearcherServiceServer).Search(m, &searcherServiceSearchServer{stream})
}

type SearcherService_SearchServer interface {
	Send(*SearchResponse) error
	grpc.ServerStream
}

type searcherServiceSearchServer struct {
	grpc.ServerStream
}

func (x *searcherServiceSearchServer) Send(m *SearchResponse) error {
	return x.ServerStream.SendMsg(m)
}

// SearcherService_ServiceDesc is the grpc.ServiceDesc for SearcherService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var SearcherService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "searcher.v1.SearcherService",
	HandlerType: (*SearcherServiceServer)(nil),
	Methods:     []grpc.MethodDesc{},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Search",
			Handler:       _SearcherService_Search_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "searcher.proto",
}
