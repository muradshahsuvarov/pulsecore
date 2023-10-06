package gateway

import (
	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	// Assume some health check endpoint
	r.GET("/gateway/health", checkHealth)

	r.Run(":8095")
}

func checkHealth(g *gin.Context) {
	// Check the micro-service health
}
