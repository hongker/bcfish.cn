package main

import (
	"bcfish.cn/demo/web/middleware"
	"fmt"
	"github.com/gin-gonic/gin"
)

func main() {

	// 初始化fabric Sdk
	fabricSetup := middleware.GetFabricSetupInstance()
	if err := fabricSetup.Initialize();err != nil {
		fmt.Printf("Unable to initialize the Fabric SDK: %v\n", err)
		return
	}

	// 安装example链码
	exampleFabricSetup, err := middleware.InitExampleCC(fabricSetup)
	if err != nil {
		fmt.Printf("初始化失败: %v\n", err)
		return
	}

	fmt.Println(exampleFabricSetup)

	r := gin.Default()
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})
	r.Run() // listen and serve on 0.0.0.0:8080

}

