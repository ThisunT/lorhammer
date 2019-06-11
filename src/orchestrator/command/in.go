package command

import (
	"encoding/json"
	"fmt"
	"lorhammer/src/model"
	"lorhammer/src/tools"

	"github.com/sirupsen/logrus"
)

var loggerIn = logrus.WithField("logger", "orchestrator/command/in")

//ApplyCmd launch a model.CMD received from a lorhammer
func ApplyCmd(command model.CMD, mqtt tools.Mqtt, provision func(model.Register) error, newLorhammer func(model.NewLorhammer) error) error {
	switch command.CmdName {
	case model.NEWLORHAMMER:  //if matches with command NEWLORHAMMER
		{
			var instance model.NewLorhammer  //has a CallbackTopic
			if err := json.Unmarshal(command.Payload, &instance); err != nil {  //attaching the topic to it
				return err
			}
			loggerIn.WithField("topic", instance.CallbackTopic).Info("New Lorhammer")
			newLorhammer(instance) //orchestrator/commands.go
			return mqtt.PublishCmd(instance.CallbackTopic, model.LORHAMMERADDED)  //src/tools/mqtt.go /publishes the command->LORHAMMERADDED on the specified topic
		}
	case model.REGISTER:
		var sensorsToRegister model.Register //orchestrator/model/protocol.go -> register struct
		if err := json.Unmarshal(command.Payload, &sensorsToRegister); err != nil {
			return err
		}
		loggerIn.WithField("nbGateways", len(sensorsToRegister.Gateways)).Info("Received registration command")  //prints the registration received

		if err := provision(sensorsToRegister); err != nil {    //contains scenario uuid, gateway struct and CallbackTopic and provisions the thing and call the provision function in the parameter
			return err
		}
		loggerIn.WithField("nbGateways", len(sensorsToRegister.Gateways)).Info("Provisioning done")

		//once provisioning is done, start the scenario
		startMessage := model.Start{
			ScenarioUUID: sensorsToRegister.ScenarioUUID,  //storing the scenariouuid in the startMessage
		}

		if err := mqtt.PublishSubCmd(sensorsToRegister.CallBackTopic, model.START, startMessage); err != nil {  //mqtt.go
			return err
		}
		loggerIn.Info("Start message sent")

	default:
		return fmt.Errorf("Unknown command %s", command.CmdName)
	}
	return nil
}
