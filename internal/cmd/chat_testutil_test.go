package cmd

import (
	"context"

	"google.golang.org/api/chat/v1"

	"github.com/steipete/gogcli/internal/app"
)

var chatTestServices = googleServiceTestSupport[chat.Service, app.ChatServiceFactory]{
	newService: chat.NewService,
	wrap: func(factory func(context.Context, string) (*chat.Service, error)) app.ChatServiceFactory {
		return factory
	},
	services: func(factory app.ChatServiceFactory) app.Services {
		return app.Services{Chat: factory}
	},
}

var (
	newChatTestService                = chatTestServices.new
	fixedChatTestService              = chatTestServices.fixed
	unexpectedChatTestService         = chatTestServices.unexpected
	executeWithChatTestService        = chatTestServices.executeWithService
	executeWithChatTestServiceFactory = chatTestServices.execute
)
