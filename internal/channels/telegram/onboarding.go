package telegram

import (
	"context"
	"fmt"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const (
	onboardingCallbackPrefix = "ob:v1:"
	onboardingActionHome     = "home"
	onboardingActionAsk      = "ask"
	onboardingActionExamples = "examples"
	onboardingActionSources  = "sources"
)

const onboardingWelcome = `👋 Привет! Я помогу найти опыт владельцев LiXiang в архиве Telegram-сообществ: обсуждения зарядки, обновлений, обслуживания и неисправностей.

🔎 Соберу ответ по найденным сообщениям и приложу ссылки. Если данных не хватит — скажу прямо. Опыт из чатов не заменяет диагностику.

🚗 Напишите вопрос своими словами. Для точности добавьте модель, год и версию ПО, если знаете.

💡 Например: «L7, 2024: зимой медленно заряжается — что обсуждали владельцы?»`

func OnboardingMenuCommands() []telego.BotCommand {
	return []telego.BotCommand{
		{Command: "start", Description: "Что умеет бот"},
		{Command: "help", Description: "Помощь и примеры"},
	}
}

func onboardingText(action string) (string, bool) {
	switch action {
	case onboardingActionHome:
		return onboardingWelcome, true
	case onboardingActionAsk:
		return "✍️ Напишите, что хотите узнать. Если вопрос о вашей машине, добавьте модель, год и версию ПО — если знаете.", true
	case onboardingActionExamples:
		return "💡 Примеры вопросов:\n\n• L7, 2024: зимой медленно заряжается — что обсуждали владельцы?\n• Что изменилось после обновления 8.1 и какие проблемы отмечали?\n• Какой опыт обслуживания кондиционера у владельцев LiXiang?", true
	case onboardingActionSources:
		return "🔎 Я ищу только в подключённом архиве Telegram-сообществ LiXiang, кратко пересказываю найденное и даю ссылки на исходные сообщения. Некоторые ссылки открываются только участникам соответствующей группы. Если подтверждений нет, я сообщу об этом прямо.", true
	default:
		return "", false
	}
}

func onboardingKeyboard(action string) *telego.InlineKeyboardMarkup {
	if action == onboardingActionHome {
		return &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{
			{{Text: "✍️ Задать вопрос", CallbackData: onboardingCallbackPrefix + onboardingActionAsk}},
			{
				{Text: "💡 Примеры", CallbackData: onboardingCallbackPrefix + onboardingActionExamples},
				{Text: "🔗 Об источниках", CallbackData: onboardingCallbackPrefix + onboardingActionSources},
			},
		}}
	}
	return &telego.InlineKeyboardMarkup{InlineKeyboard: [][]telego.InlineKeyboardButton{
		{{Text: "↩️ Назад", CallbackData: onboardingCallbackPrefix + onboardingActionHome}},
	}}
}

func (c *Channel) sendOnboarding(ctx context.Context, chatID int64, action string, setThread func(*telego.SendMessageParams)) {
	text, ok := onboardingText(action)
	if !ok {
		return
	}
	msg := tu.Message(tu.ID(chatID), text)
	msg.ReplyMarkup = onboardingKeyboard(action)
	if setThread != nil {
		setThread(msg)
	}
	_, _ = c.bot.SendMessage(ctx, msg)
}

func (c *Channel) onboardingCallbackAllowed(query *telego.CallbackQuery) bool {
	if query == nil || !c.config.OnboardingEnabled || c.config.DMPolicy != "allowlist" || query.Message == nil || !query.Message.IsAccessible() {
		return false
	}
	if query.Message.GetChat().Type != "private" {
		return false
	}
	if !c.HasAllowList() {
		return false
	}
	return c.IsAllowed(fmt.Sprintf("%d", query.From.ID))
}

func (c *Channel) handleOnboardingCallback(ctx context.Context, query *telego.CallbackQuery) {
	if !c.onboardingCallbackAllowed(query) {
		return
	}
	action := strings.TrimPrefix(query.Data, onboardingCallbackPrefix)
	if _, ok := onboardingText(action); !ok {
		return
	}
	message := query.Message.Message()
	setThread := func(msg *telego.SendMessageParams) {
		if message != nil && message.MessageThreadID > 0 {
			msg.MessageThreadID = message.MessageThreadID
		}
	}
	c.sendOnboarding(ctx, query.Message.GetChat().ID, action, setThread)
}
