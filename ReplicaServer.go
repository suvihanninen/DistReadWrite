package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"time"

	// this has to be the same as the go.mod module,
	// followed by the path to the folder the proto file is in.
	distWriter "github.com/suvihanninen/DistReadWrite.git/grpc"
	"google.golang.org/grpc"
)

type ReplicaServer struct {
	distWriter.UnimplementedReadWriteServer
	id             int32
	isPrimary      bool
	ctx            context.Context
	time           time.Time
	ReplicaLamport int64
	value          int32
	lock           chan bool
}

func main() {

	portInput, _ := strconv.ParseInt(os.Args[1], 10, 32) //Takes arguments 0, 1 and 2, see comment X
	ownPort := int32(portInput) + 5001
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	println(ownPort)
	println(portInput)
	server := &ReplicaServer{
		id:    ownPort,
		ctx:   ctx,
		value: 0,
		lock:  make(chan bool, 1),
	}

	//unlock
	server.lock <- true

	//log to file instead of console
	f := setLogRMServer()
	defer f.Close()

	println("Making connection in order to listen other replicas")
	list, err := net.Listen("tcp", fmt.Sprintf(":%v", ownPort))
	if err != nil {
		log.Fatalf("Failed to listen on port: %v", err)
	}
	println("Registered the new server")
	grpcServer := grpc.NewServer()
	distWriter.RegisterReadWriteServer(grpcServer, server)
	println("Server listening on port: ", ownPort)
	go func() {
		if err := grpcServer.Serve(list); err != nil {
			log.Fatalf("failed to server %v", err)
		}
	}()
	for {
	}
}

func (RS *ReplicaServer) Write(ctx context.Context, WriteRequest *distWriter.WriteRequest) (*distWriter.WriteResponse, error) {
	<-RS.lock
	RS.value = WriteRequest.GetValue()
	updatedLamport := RS.updateLamportTime(WriteRequest.GetLamport())
	log.Printf("ReplicaServer %v: writing value: %v", RS.id, RS.value)
	RS.lock <- true
	return &distWriter.WriteResponse{Ack: "Success",
		Lamport: updatedLamport}, nil

}

func (RS *ReplicaServer) Read(ctx context.Context, ReadRequest *distWriter.ReadRequest) (*distWriter.ReadResponse, error) {
	//<-RS.lock
	log.Printf("ReplicaServer %v: reading value: %v", RS.id, RS.value)
	updatedLamport := RS.updateLamportTime(ReadRequest.GetLamport())
	//RS.lock <- true
	return &distWriter.ReadResponse{Value: RS.value,
		Lamport: updatedLamport}, nil

}

func (RS *ReplicaServer) updateLamportTime(incomingLamport int64) int64 {
	if RS.ReplicaLamport > incomingLamport {
		RS.ReplicaLamport = RS.ReplicaLamport + 1
	} else {
		RS.ReplicaLamport = incomingLamport + 1
	}
	return RS.ReplicaLamport
}

func setLogRMServer() *os.File {
	f, err := os.OpenFile("log.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	log.SetOutput(f)
	return f
}
