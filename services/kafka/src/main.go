package kafka

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()

	r.POST("/kafka/publish", publishMessage)
	r.GET("/kafka/consume", consumeMessage)
	r.GET("/kafka/health", checkHealth)

	r.Run(":8096")
}

func publishMessage(g *gin.Context) {

}

func consumeMessage(g *gin.Context) {

}

func checkHealth(g *gin.Context) {
	// Check the micro-service health
}
