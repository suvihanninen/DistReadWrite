package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	// this has to be the same as the go.mod module,
	// followed by the path to the folder the proto file is in.
	distWriter "github.com/suvihanninen/DistReadWrite.git/grpc"
	"google.golang.org/grpc"
)

type FEServer struct {
	distWriter.UnimplementedReadWriteServer        // You need this line if you have a server
	port                                    string // Not required but useful if your server needs to know what port it's listening to
	ctx                                     context.Context
	replicas                                map[int32]distWriter.ReadWriteClient
	FELocalLamport                          int64
	noCrachedNodes                          bool
}

func main() {
	port := os.Args[1] //give it a port and input the same port to the client
	address := ":" + port
	list, err := net.Listen("tcp", address)

	if err != nil {
		log.Printf("FEServer %s: Server on port %s: Failed to listen on port %s: %v", port, port, address, err) //If it fails to listen on the port, run launchServer method again with the next value/port in ports array
		return
	}
	grpcServer := grpc.NewServer()

	//log to file instead of console
	f := setLogFEServer()
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := &FEServer{
		port:           os.Args[1],
		ctx:            ctx,
		replicas:       make(map[int32]distWriter.ReadWriteClient),
		FELocalLamport: 0,
		noCrachedNodes: true,
	}

	distWriter.RegisterReadWriteServer(grpcServer, server) //Registers the server to the gRPC server.
	//FEServer needs to dial to Replicas ---> idk if we need defer conn.Close. RN it is in DialToReplicas method

	//Make connection to 5001
	port1 := ":5001"
	portInt1 := int32(5001)
	connection1, err := grpc.Dial(port1, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Unable to connect: %v", err)
	}
	c1 := distWriter.NewReadWriteClient(connection1)
	server.replicas[portInt1] = c1

	//Make connection to 5002
	port2 := ":5002"
	portInt2 := int32(5002)
	connection2, err := grpc.Dial(port2, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Unable to connect: %v", err)
	}
	c2 := distWriter.NewReadWriteClient(connection2)
	server.replicas[portInt2] = c2

	//Make connection to 5003
	port3 := ":5003"
	portInt3 := int32(5003)
	connection3, err := grpc.Dial(port3, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Unable to connect: %v", err)
	}
	c3 := distWriter.NewReadWriteClient(connection3)
	server.replicas[portInt3] = c3

	defer connection1.Close()
	defer connection2.Close()
	defer connection3.Close()

	log.Printf("FEServer %s: Server on port %s: Listening at %v\n", server.port, port, list.Addr())
	go func() {
		log.Printf("FEServer %s: We are trying to listen calls from client: %s", server.port, port)

		if err := grpcServer.Serve(list); err != nil {
			log.Fatalf("failed to serve %v", err)
		}

		log.Printf("FEServer %s: We have started to listen calls from client: %s", server.port, port)
	}()
	for {
	}

}

func (FE *FEServer) DialToReplicas() {
	println("FEServer connectiong to Replicas")
	for i := 0; i < 3; i++ {
		port := int32(5001) + int32(i)

		var conn *grpc.ClientConn
		log.Printf("FEServer %s: Trying to dial: %v\n", FE.port, port)
		conn, err := grpc.Dial(fmt.Sprintf(":%v", port), grpc.WithInsecure(), grpc.WithBlock()) //This is going to wait until it receives the connection
		if err != nil {
			log.Fatalf("Could not connect: %s", err)
		}
		defer conn.Close()
		c := distWriter.NewReadWriteClient(conn)
		FE.replicas[port] = c

	}
	println("Connected to the Replicas")
}

func (FE *FEServer) Read(ctx context.Context, ReadRequest *distWriter.ReadRequest) (*distWriter.ReadResponse, error) {
	//Here we need to distribute the request to all Replicas

	result, err := FE.DistributeRead(ReadRequest)
	if err != nil {
		log.Fatalf("Reading value from Replica Server failed: ", err)
	}

	return result, nil
}

func (FE *FEServer) DistributeRead(ReadRequest *distWriter.ReadRequest) (*distWriter.ReadResponse, error) {
	var ackCount int //Find the quorum
	var respArray []*distWriter.ReadResponse

	for id, server := range FE.replicas {
		response, err := server.Read(FE.ctx, ReadRequest) //Reading value
		if err != nil {
			log.Printf("FEServer %v: Something went wrong when reading value from %v", FE.port, id)
			log.Printf("FEServer %v: Failure, Replica on port %v died", FE.port, id)
			FE.noCrachedNodes = false
			delete(FE.replicas, id)
			continue
		}
		ackCount++

		respArray = append(respArray, response)
		if ackCount >= 2 { //find the value that the quorum has
			log.Printf("FEServer %v: Finding the value which the quorum of the replicas have", FE.port)
			if respArray[0].GetValue() == respArray[1].GetValue() {
				log.Printf("FEServer %v: 1Quorum of replicas has value: %v", FE.port, respArray[0].GetValue())
				return &distWriter.ReadResponse{Value: respArray[0].GetValue()}, nil
			} else if len(respArray) == 3 && respArray[1].GetValue() == respArray[2].GetValue() && FE.noCrachedNodes {
				log.Printf("FEServer %v: 2Quorum of replicas has value: %v", FE.port, respArray[1].GetValue())
				return &distWriter.ReadResponse{Value: respArray[1].GetValue()}, nil
			} else if len(respArray) == 3 && respArray[2].GetValue() == respArray[0].GetValue() && FE.noCrachedNodes {
				log.Printf("FEServer %v: 3Quorum of replicas has value: %v", FE.port, respArray[2].GetValue())
				return &distWriter.ReadResponse{Value: respArray[2].GetValue()}, nil
			}
			//If any of the values that the system read from the replicas go not match the program will run here
			if len(FE.replicas) == 2 {
				//if a replica has crashed, the system will call again to DistributeRead to find matching values
				//if write operation was excecuted in parallel with read operation
				time.Sleep(2 * time.Second)
				log.Printf("Recalling DistributeRead to get consistent result from the replicas")
				_, err := FE.DistributeRead(ReadRequest)
				if err != nil {
					log.Fatalf("Reading value from Replica Server failed: ", err)
				}
			}

		}

	}
	return &distWriter.ReadResponse{Value: respArray[0].GetValue()}, nil
}

func (FE *FEServer) Write(ctx context.Context, WriteRequest *distWriter.WriteRequest) (*distWriter.WriteResponse, error) {
	//Here we need to distribute the request to all Replicas
	result, err := FE.DistributeWrite(WriteRequest)
	if err != nil {
		log.Fatalf("Writing value to Replica failed inside FEServer: %s", err)
	}

	return result, nil

}

func (FE *FEServer) DistributeWrite(WriteRequest *distWriter.WriteRequest) (*distWriter.WriteResponse, error) {
	//We need a quorum response in order to know that quorum of the nodes have updated the value
	var ackCount int //Find the quorum
	successUpdate := true
	for id, server := range FE.replicas {
		time.Sleep(1 * time.Second)
		response, err := server.Write(FE.ctx, WriteRequest)
		if err != nil {
			log.Printf("FEServer %v: Failure, Replica on port %v died", FE.port, id)
			log.Printf("FEServer %v: Write response from %v: fail", FE.port, id)
			successUpdate = false
			delete(FE.replicas, id)
		}

		if successUpdate {
			log.Printf("FEServer %v: Write response from %v: %s", FE.port, id, response.GetAck())
			ackCount++
			if ackCount == 3 {
				//Quorum has written the value successfully but we still need to update the rest of the replicas
				return &distWriter.WriteResponse{Ack: "Success"}, nil
			}
		}
		successUpdate = true
	}

	return &distWriter.WriteResponse{Ack: "Success"}, nil
}

func (FE *FEServer) updateLamportTime(incomingLamport int64) int64 {
	if FE.FELocalLamport > incomingLamport {
		FE.FELocalLamport = FE.FELocalLamport + 1
	} else {
		FE.FELocalLamport = incomingLamport + 1
	}
	return FE.FELocalLamport
}

// sets the logger to use a log.txt file instead of the console
func setLogFEServer() *os.File {
	f, err := os.OpenFile("log.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	log.SetOutput(f)
	return f
}
