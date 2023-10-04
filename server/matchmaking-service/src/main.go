package matchmaking

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()

	r.GET("/matchmaking/health", checkHealth)

	r.Run(":8096")
}

func checkHealth(g *gin.Context) {
	// Check the micro-service health
}
