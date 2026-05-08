package models

import (
	"github.com/gin-gonic/gin"
)

type CommonResponse struct {
	Status  bool   `json:"status"`
	Data    any    `json:"data"`
	Message string `json:"message"`
}

func (cm CommonResponse) Success(context *gin.Context, httpStatus int, message string, data any) {
	cm.Status = true
	cm.Message = message
	if data != nil {
		cm.Data = data
	} else {
		cm.Data = gin.H{}
	}
	context.JSON(httpStatus, cm)
}

func (cm CommonResponse) Failure(context *gin.Context, httpStatus int, message string) {
	cm.Status = false
	cm.Message = message
	cm.Data = gin.H{}
	context.AbortWithStatusJSON(httpStatus, cm)
}

func (cm CommonResponse) FailureWithoutData(context *gin.Context, httpStatus int, message string, data any) {
	cm.Status = false
	cm.Message = message
	cm.Data = data
	context.AbortWithStatusJSON(httpStatus, cm)
}
