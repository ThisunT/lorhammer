package tools

import (
	"errors"
	"testing"

	mqttLib "github.com/eclipse/paho.mqtt.golang"
)

type fakeSubMqtt struct{}

func (fakeSubMqtt) IsConnected() bool       { return true }
func (fakeSubMqtt) Connect() mqttLib.Token  { return nil }
func (fakeSubMqtt) Disconnect(quiesce uint) {}
func (fakeSubMqtt) Publish(topic string, qos byte, retained bool, payload interface{}) mqttLib.Token {
	return nil
}
func (fakeSubMqtt) Subscribe(topic string, qos byte, callback mqttLib.MessageHandler) mqttLib.Token {
	return nil
}
func (fakeSubMqtt) SubscribeMultiple(filters map[string]byte, callback mqttLib.MessageHandler) mqttLib.Token {
	return nil
}
func (fakeSubMqtt) Unsubscribe(topics ...string) mqttLib.Token {
	return nil
}
func (fakeSubMqtt) AddRoute(topic string, callback mqttLib.MessageHandler) {}

func newMqtt(t *testing.T) Mqtt {
	mqtt, err := NewMqtt("", fakeConsul{})
	if err != nil {
		t.Fatal("Valid mqtt config should not throw error")
	}
	if mqtt == nil {
		t.Fatal("Valid mqtt config should return mqtt client")
	}
	mqtt.(*mqttImpl).client = fakeSubMqtt{}
	return mqtt
}

func TestNewMqtt(t *testing.T) {
	newMqtt(t)
}

func TestNewMqttNotFound(t *testing.T) {
	mqtt, err := NewMqtt("", fakeConsul{serviceFirstError: errors.New("error")})
	if err == nil {
		t.Fatal("If consul not return mqtt url, mqtt should throw error")
	}
	if mqtt != nil {
		t.Fatal("If consul not return mqtt url, mqtt should not return client")
	}
}
