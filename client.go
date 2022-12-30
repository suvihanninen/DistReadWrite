package main

import (
	"bufio"
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	// this has to be the same as the go.mod module,
	// followed by the path to the folder the proto file is in.
	distWriter "github.com/suvihanninen/DistReadWrite.git/grpc"
	"google.golang.org/grpc"
)

var server distWriter.ReadWriteClient //the server
var ServerConn *grpc.ClientConn       //the server connection

func main() {
	port := ":" + os.Args[1]
	connection, err := grpc.Dial(port, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Unable to connect: %v", err)
	}

	//log to file instead of console
	f := setLogClient()
	defer f.Close()

	server = distWriter.NewReadWriteClient(connection) //creates a new client

	defer connection.Close()

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		println("Insert 'read' or 'write [value]'")

		for {
			scanner.Scan()
			text := scanner.Text()

			if strings.Contains(text, "write") {
				input := strings.Fields(text)

				value, err := strconv.ParseInt(input[1], 10, 32)
				if err != nil {
					log.Fatalf("Problem with key: %v", err)
				}

				writeVal := &distWriter.WriteRequest{
					Value: int32(value),
				}

				ack := Write(writeVal, connection, server, port)

				log.Printf("Client %s: Write response: %v", port, ack)
				println("Client " + port + ": Write response: " + ack)
			} else if text == "read" {

				readRequest := &distWriter.ReadRequest{}
				for i := 0; i < 5; i++ {
					time.Sleep(2 * time.Second)
					log.Printf("Client %s: %v. read request", port, i+1)
					value := Read(readRequest, connection, server, port)
					valueString := strconv.FormatInt(int64(value), 10)
					log.Printf("Client %s: Result from read request: %s", port, valueString)
					println("Client " + port + ": Result from READ request: " + valueString)
				}
			} else {
				println("Sorry didn't catch that, try again ")
			}

		}
	}()

	for {

	}

}

func Write(writeRequest *distWriter.WriteRequest, connection *grpc.ClientConn, server distWriter.ReadWriteClient, port string) string {
	var acknowledgement string
	log.Printf("Client %s: Write a new value ", port)
	ack, err := server.Write(context.Background(), writeRequest)
	if err != nil {
		log.Printf("Client %s: Write failed: ", port, err)
		log.Printf("Client %s: FEServer has died", port)
		connection, server = Redial(port)
		acknowledgement = Write(writeRequest, connection, server, port)
		return acknowledgement
	}
	acknowledgement = ack.GetAck()
	return acknowledgement
}

func Read(readRequest *distWriter.ReadRequest, connection *grpc.ClientConn, server distWriter.ReadWriteClient, port string) int32 {

	var value int32
	val, err := server.Read(context.Background(), readRequest)
	if err != nil {
		log.Printf("Client %s: Read failed: ", port, err)
		log.Printf("Client %s: FEServer has died", port)
		connection, server = Redial(port)
		value = Read(readRequest, connection, server, port)
		return value
	}
	value = val.GetValue()
	return value
}

func Redial(port string) (*grpc.ClientConn, distWriter.ReadWriteClient) {
	log.Printf("Client: FEServer on port %s is not listening anymore. It has died", port)
	if port == ":4001" {
		port = ":4002"
	} else {
		port = ":4001"
	}
	log.Printf("Client: Redialing to new port: " + port)
	connection, err := grpc.Dial(port, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Unable to connect: %v", err)
	}

	server = distWriter.NewReadWriteClient(connection) //creates a new client

	log.Printf("Client: Client has connected to new FEServer on port %s", port)
	return connection, server
}

// sets the logger to use a log.txt file instead of the console
func setLogClient() *os.File {
	f, err := os.OpenFile("log.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening file: %v", err)
	}
	log.SetOutput(f)
	return f
}
