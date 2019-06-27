package lora

import (
	"encoding/hex"
	"errors"
	"lorhammer/src/model"
	"lorhammer/src/tools"
	"github.com/brocaar/lorawan"
	"github.com/sirupsen/logrus"
)

var loggerNode = logrus.WithFields(logrus.Fields{"logger": "lorhammer/lora/node"})

func newNode(nwsKeyStr string, appsKeyStr string, description string, payloads []model.Payload, randomPayloads bool) *model.Node {

	devEui := tools.Random8Bytes()

	nwsKey := lorawan.AES128Key{}
	if nwsKeyStr != "" {
		nwsKey.UnmarshalText([]byte(nwsKeyStr))
	}

	appsKey := lorawan.AES128Key{}
	if appsKeyStr != "" {
		appsKey.UnmarshalText([]byte(appsKeyStr))
	}

	return &model.Node{
		DevEUI:         devEui,
		AppEUI:         tools.Random8Bytes(),
		AppKey:         getGenericAES128Key(),
		DevAddr:        getDevAddrFromDevEUI(devEui),
		AppSKey:        appsKey,
		NwSKey:         nwsKey,
		Payloads:       payloads,
		NextPayload:    0,
		RandomPayloads: randomPayloads,
		Description:    description,
		DevNonce:				tools.Random2Bytes(),
	}
}

func getJoinRequestDataPayload(node *model.Node) []byte {

	phyPayload := lorawan.PHYPayload{
		MHDR: lorawan.MHDR{
			MType: lorawan.MType(lorawan.JoinRequest),
			Major: lorawan.Major(byte(0)),
		},

		MACPayload: &lorawan.JoinRequestPayload{
			AppEUI:   node.AppEUI,
			DevEUI:   node.DevEUI,
			DevNonce: node.DevNonce,
		},
	}

	err := phyPayload.SetMIC(node.AppKey)

	if err != nil {
		loggerNode.WithFields(logrus.Fields{
			"ref": "lorhammer/lora/payloadFactory:NewJoinRequestPHYPayload()",
			"err": err,
		}).Error("Could not calculate MIC")
	}

	b, err := phyPayload.MarshalBinary()
	if err != nil {
		loggerNode.Error("unable to marshal physical payload")
		return []byte{}
	}
	return b
}

func GetPushDataPayload(node *model.Node, fcnt uint32) ([]byte, int64, error) {
	fport := uint8(2)

	var frmPayloadByteArray []byte
	var date int64

	if len(node.Payloads) == 0 {
		loggerNode.WithFields(logrus.Fields{
			"DevEui": node.DevEUI.String(),
		}).Warn("empty payload array given. So it send `LorHammer`")
	} else {
		var i int
		if node.RandomPayloads == true {
			i = tools.Random(0, len(node.Payloads)-1)
		} else {
			i = node.NextPayload
			if len(node.Payloads) >= i-1 {
				node.NextPayload = i + 1
			}
			// if the current payload is the last of the payload set, a complete round has been executed
			if len(node.Payloads) == node.NextPayload {
				node.PayloadsReplayLap++
				loggerNode.WithFields(logrus.Fields{
					"DevEui":            node.DevEUI.String(),
					"PayloadsReplayLap": node.PayloadsReplayLap,
				}).Info("Complete lap executed")
				node.NextPayload = 0
			}
			date = node.Payloads[i].Date
		}
		loggerNode.WithFields(logrus.Fields{
			"DevEui":             node.DevEUI.String(),
			"Valeur de i":        i,
			"len(node.Payloads)": len(node.Payloads),
			"node.NextPayload":   node.NextPayload,
			"Payload : ":         node.Payloads[i].Value,
			"Date : ":            node.Payloads[i].Date,
		}).Debug("Payload sent")

		frmPayloadByteArray, _ = hex.DecodeString(node.Payloads[i].Value)
	}

	phyPayload := lorawan.PHYPayload{
		MHDR: lorawan.MHDR{
			MType: lorawan.MType(lorawan.ConfirmedDataUp),
			Major: lorawan.LoRaWANR1,
		},

		MACPayload: &lorawan.MACPayload{
			FHDR: lorawan.FHDR{
				DevAddr: node.DevAddr,
				FCtrl: lorawan.FCtrl{
					ADR:       false,
					ADRACKReq: false,
					ACK:       false,
				},
				FCnt: fcnt,
			},
			FPort:      &fport,
		  FRMPayload: []lorawan.Payload{&lorawan.DataPayload{Bytes: frmPayloadByteArray}},
		},
	}

	err := phyPayload.EncryptFRMPayload(node.AppSKey)
	if err != nil {
		loggerNode.WithFields(logrus.Fields{
			"ref": "lorhammer/lora/payloadFactory:GetPushDataPayload()",
			"err": err,
		}).Fatal("Could not encrypt FRMPayload")
	}

	err = phyPayload.SetMIC(node.NwSKey)
	if err != nil {
		loggerNode.WithFields(logrus.Fields{
			"ref": "lorhammer/lora/payloadFactory:GetPushDataPayload()",
			"err": err,
		}).Fatal("Could not calculate MIC")
	}

	b, err := phyPayload.MarshalBinary()
	if err != nil {
		return nil, 0, errors.New("unable to marshal physical payload")
	}
	return b, date, nil
}

func getMACCmdLinkCheckReqDataPayload(node *model.Node) []byte {

	phyPayload := lorawan.PHYPayload{
		MHDR: lorawan.MHDR{
			MType: lorawan.UnconfirmedDataUp,
			Major: lorawan.LoRaWANR1,
		},

		MACPayload: &lorawan.MACPayload{
			FHDR: lorawan.FHDR{
				DevAddr: node.DevAddr,
				FOpts: []lorawan.MACCommand{
					{
						CID: lorawan.LinkCheckReq,
					},
				},
			},
		},
	}

	err := phyPayload.SetMIC(node.NwSKey)
	if err != nil {
		loggerNode.WithFields(logrus.Fields{
			"ref": "lorhammer/lora/payloadFactory:getMACCmdLinkCheckReqDataPayload()",
			"err": err,
		}).Fatal("Could not calculate MIC")
	}

	b, err := phyPayload.MarshalBinary()
	if err != nil {
		return nil
	}
	return b
}

func getMACCmdLinkADRReqDataPayload(node *model.Node) []byte {

	// phyPayload := lorawan.PHYPayload{
	// 	MHDR: lorawan.MHDR{
	// 		MType: lorawan.UnconfirmedDataUp,
	// 		Major: lorawan.LoRaWANR1,
	// 	},
	// 	MACPayload: &lorawan.MACPayload{
	// 		FHDR: lorawan.FHDR{
	// 			DevAddr: node.DevAddr,
	// 			FCnt:    10,
	// 			FOpts: []lorawan.MACCommand{
	// 				{CID: lorawan.LinkADRAns, Payload: &lorawan.LinkADRAnsPayload{ChannelMaskACK: true, DataRateACK: true, PowerACK: true}},
	// 			},
	// 		},
	// 	},
	// }

	phyPayload := &lorawan.PHYPayload{
		MHDR: lorawan.MHDR{
			MType: lorawan.UnconfirmedDataDown,
			Major: lorawan.LoRaWANR1,
		},
		MACPayload: &lorawan.MACPayload{
			FHDR: lorawan.FHDR{
				DevAddr: node.DevAddr,
				FCnt:    5,
				FCtrl: lorawan.FCtrl{
					ADR: true,
				},
				FOpts: []lorawan.MACCommand{
					{
						CID: lorawan.LinkADRReq,
						Payload: &lorawan.LinkADRReqPayload{
							TXPower: 0,
							ChMask:  lorawan.ChMask{true, true, true},
						},
					},
				},
			},
		},
	}

	// phyPayload := &lorawan.PHYPayload{
	// 	MHDR: lorawan.MHDR{
	// 		MType: lorawan.UnconfirmedDataDown,
	// 		Major: lorawan.LoRaWANR1,
	// 	},
	// 	MACPayload: &lorawan.MACPayload{
	// 		FHDR: lorawan.FHDR{
	// 			DevAddr: node.DevAddr,
	// 			FCnt:    5,
	// 			FCtrl: lorawan.FCtrl{
	// 				ADR: true,
	// 			},
	// 			FOpts: []lorawan.MACCommand{
	// 				{
	// 					CID: lorawan.LinkADRReq,
	// 					Payload: &lorawan.LinkADRReqPayload{
	// 						DataRate: 5,
	// 						TXPower:  2,
	// 						ChMask:   [16]bool{true, true, true},
	// 						Redundancy: lorawan.Redundancy{
	// 							ChMaskCntl: 0,
	// 							NbRep:      1,
	// 						},
	// 					},
	// 				},
	// 			},
	// 		},
	// 	},
	// }

	err := phyPayload.SetMIC(node.NwSKey)
	if err != nil {
		loggerNode.WithFields(logrus.Fields{
			"ref": "lorhammer/lora/payloadFactory:getMACCmdLinkADRReqDataPayload()",
			"err": err,
		}).Fatal("Could not calculate MIC")
	}

	b, err := phyPayload.MarshalBinary()
	if err != nil {
		return nil
	}
	return b
}

func getMACCmdDevStatusAnsDataPayload(node *model.Node) []byte {
		fport := uint8(2)

	phyPayload := lorawan.PHYPayload{
		MHDR: lorawan.MHDR{
			MType: lorawan.UnconfirmedDataUp,
			Major: lorawan.LoRaWANR1,
		},
		MACPayload: &lorawan.MACPayload{
			FHDR: lorawan.FHDR{
				DevAddr: node.DevAddr,
				FCnt:    10,
				FOpts: []lorawan.MACCommand{
					{
						CID: lorawan.DevStatusAns,
						Payload: &lorawan.DevStatusAnsPayload{
							Battery: 128,
							Margin:  10,
						},
					},
				},
			},
			FPort:      &fport,
			FRMPayload: []lorawan.Payload{&lorawan.DataPayload{Bytes: []byte{1, 2, 3, 4}}},
		},
	}

	err := phyPayload.SetMIC(node.NwSKey)
	if err != nil {
		loggerNode.WithFields(logrus.Fields{
			"ref": "lorhammer/lora/payloadFactory:getMACCmdDevStatusReqDataPayload()",
			"err": err,
		}).Fatal("Could not calculate MIC")
	}

	b, err := phyPayload.MarshalBinary()
	if err != nil {
		return nil
	}
	return b
}

func getDevAddrFromDevEUI(devEUI lorawan.EUI64) lorawan.DevAddr {
	devAddr := lorawan.DevAddr{}
	devEuiStr := devEUI.String()
	devAddr.UnmarshalText([]byte(devEuiStr[len(devEuiStr)-8:]))
	return devAddr
}

func getGenericAES128Key() lorawan.AES128Key {
	return lorawan.AES128Key{
		byte(1), byte(2), byte(3), byte(4),
		byte(5), byte(6), byte(7), byte(8),
		byte(12), byte(11), byte(10), byte(9),
		byte(13), byte(14), byte(15), byte(16),
	}
}
