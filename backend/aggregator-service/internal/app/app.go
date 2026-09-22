package app

import (
	"aggregator-service/internal/config"
	"aggregator-service/internal/service"
	pb "aggregator-service/internal/transport/rpc/gen"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func StartApp() {
	cfg := config.LoadConfig()
	conn, err := grpc.NewClient(
		cfg.SourceCraftWrapperURL,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	clientRPC := pb.NewCodeHealthClient(conn)
	aggregatorService := service.NewAggregatorService(
		reposRepo,
		gradesRepo,
		llmRepo,
		clientRPC,
	)
}
