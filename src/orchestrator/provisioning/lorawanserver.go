package provisioning

import (
	"bytes"
	"context"
	//"crypto/tls"
	"encoding/json"
	"errors"
	"io/ioutil"
	"lorhammer/src/model"
	//"net"
	"fmt"
	"net/http"
	"time"
	"github.com/brocaar/lorawan"
  "github.com/bobziuchkovski/digest"
	"github.com/sirupsen/logrus"
)

var logLorawan_server = logrus.WithField("logger", "orchestrator/provisioning/lorawan_server") //logger for lorawan_server

const (
	lorawan_serverType             = Type("lorawan_server")
	lorawan_serverAreaName = "SUTD"
	lorawan_serverNetworkName = "lorhammer"
	lorawan_serverNetworkRegion = "AS923"
	lorawan_serverGroupName = "cs2_lorhammer"
	lorawan_serverProfileName = "cs2_lorhammer_profile"
	httpLorawan_serverTimeout      = 1 * time.Minute
)

type httpClientSeender interface {  //for http requests/response
	Do(*http.Request) (*http.Response, error)
}

type lorawan_server struct {
	APIURL                string `json:"apiUrl"`
	Login                 string `json:"login"` //req??
	Password              string `json:"password"`//req??
	//OrganizationID        string `json:"organizationId"`
	//NetworkServerID       string `json:"networkServerId"`
	//NetworkServerAddr     string `json:"networkServerAddr"`
	//ServiceProfileID      string `json:"serviceProfileID"`
	//AppID                 string `json:"appId"`
	//DeviceProfileID       string `json:"deviceProfileId"`
	Abp                   bool   `json:"abp"`
	NbProvisionerParallel int    `json:"nbProvisionerParallel"`
	DeleteOrganization    bool   `json:"deleteOrganization"`
	DeleteApplication     bool   `json:"deleteApplication"`
	AreaName              string `json:"areaName"`
  NetworkName           string `json:"networkName"`
  GroupName             string `json:"groupName"`
  ProfileName           string `json:"profileName"`
	httpClient httpClientSeender
}

//gets rawConfig json and returns the config
func newLorawan_server(rawConfig json.RawMessage) (provisioner, error) {
	//t := digest.NewTransport(lorawan_server.login, lorawan_server.password)
	t := digest.NewTransport("admin", "admin")
	config := &lorawan_server{  //reference struct of lorawan_server defined above
		httpClient:&http.Client{
		Transport:t,
		},
	}
	if err := json.Unmarshal(rawConfig, config); err != nil {
		return nil, err
	}

	return config, nil //contains info from the json into structure config
}//only has httpClient value, rest are not initialized


func (lorawan_server *lorawan_server) Provision(sensorsToRegister model.Register) error {

	if err:=lorawan_server.initAreaName();err!=nil{
	//fmt.Println("Area err",err)
	return err
 }
  if err:=lorawan_server.initNetworkName();err!=nil{
	//fmt.Println("Network err",err)
	return err
	}

	if err:=lorawan_server.initGroups();err!=nil{
    //fmt.Println("Group err",err)
		return err
  }

	if err:=lorawan_server.initProfile();err!=nil{
    //fmt.Println("Profiles err",err)
		return err
  }

  //counting the total number of nodes for all the Gateways in nbNodeToProvision
	nbNodeToProvision := 0
	for _, gateway := range sensorsToRegister.Gateways {
		if err:=lorawan_server.initGateway(gateway.MacAddress);err!=nil{
			return err
		}
		for range gateway.Nodes {
			nbNodeToProvision++
		}
	}

 //channels for relaying messages in a concurrent manner
	sensorChan := make(chan *model.Node, nbNodeToProvision) //channel for sharing information for all nodes
	defer close(sensorChan)
	poison := make(chan bool, lorawan_server.NbProvisionerParallel)//Number of parallel request will access loraserver to provision.
	defer close(poison)
	errorChan := make(chan error)
	defer close(errorChan)
	sensorFinishChan := make(chan *model.Node)
	defer close(sensorFinishChan)

	// to prevent the error of devices having same devnonce delete previously commissioned devices (OTAA)
	if !lorawan_server.Abp {
		type Device struct{
			DevEUI string `json:"deveui"`
		}
		var dev []Device
		lorawan_server.doRequest(lorawan_server.APIURL+"/api/devices/", "GET", nil, &dev)
		if len(dev) > 0{
			for _, device := range dev {
				lorawan_server.doRequest(lorawan_server.APIURL+"/api/devices/"+device.DevEUI, "DELETE", nil, nil)
			}
		}
	}

	//concurrently provisioning all the Sensors parallely for NbProvisionerParallel times
	for i := 0; i < lorawan_server.NbProvisionerParallel; i++ {
		go lorawan_server.provisionSensorAsync(sensorChan, poison, errorChan, sensorFinishChan)
	}
  //do
	go func() {
		for _, gateway := range sensorsToRegister.Gateways {
			gateway := gateway
			for _, sensor := range gateway.Nodes {
				sensor := sensor
				sensorChan <- sensor
			}
		}
	}()

	for i := 0; i < nbNodeToProvision; i++ {
		select {
		case err := <-errorChan:
			logLorawan_server.WithError(err).Error("Node not provisioned")
		case sensor := <-sensorFinishChan:
			logLorawan_server.WithField("node", sensor).Debug("Node provisioned")
		}
	}

	for i := 0; i < lorawan_server.NbProvisionerParallel; i++ {
		poison <- true
	}

	return nil
}

func (lorawan_server *lorawan_server) initAreaName()error{
  if lorawan_server.AreaName==""{
    type Area struct{
      Name string `json:"name"`
    }
    var respExist []Area

    err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/areas", "GET", nil, &respExist)
		if err != nil {
      //fmt.Println("Area err2",err) //delete debugging
  		return err //delete debugging
		}

    for _, orga := range respExist {
			if orga.Name == lorawan_serverAreaName { //if orga.Name matches Lorhammer
        lorawan_server.AreaName=orga.Name
        //fmt.Println("found->",lorawan_server.AreaName)
        break
			}
		}

    if lorawan_server.AreaName==""{
      req:= struct {
    						Name string `json:"name"`
    						//Admins string `json:"admins"`
    						Log_ignored bool `json:"log_ignored"`
    					}{
    						Name:lorawan_serverAreaName,
    						//Admins:"admin",
    						Log_ignored:true,
    					}
    var resp []Area
    err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/areas", "POST", req , &resp)
    if err != nil {
      //fmt.Println("Area err3",err) //delete debugging
      return err //delete debugging
		}
    if resp==nil{
      lorawan_server.AreaName=lorawan_serverAreaName
    }

  }
}
  return nil
}

func (lorawan_server *lorawan_server) initNetworkName()error{
  if lorawan_server.NetworkName==""{
    type Network struct{
      Name string `json:"name"`
    }
    var respExist []Network

    err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/networks", "GET", nil, &respExist)
		if err != nil {
      //fmt.Println("Network err2",err) //delete debugging
  		return err //delete debugging
		}

    for _, orga := range respExist {
			if orga.Name == lorawan_serverNetworkName { //if orga.Name matches Lorhammer
        lorawan_server.NetworkName=orga.Name
        //fmt.Println("found->",lorawan_server.NetworkName)
        break
			}
		}

    if lorawan_server.NetworkName==""{
      req:= struct {
    						Name string `json:"name"`
                NetID string `json:"netid"`
    						Region string `json:"region"`
                CodingRate string `json:"tx_codr"`
                RX1JoinDelay int `json:"join1_delay"`
                RX2JoinDelay int `json:"join2_delay"`
                RX1Delay int `json:"rx1_delay"`
                RX2Delay int `json:"rx2_delay"`
                GatewayPower int `json:"gw_power"`
                MaxEIRP int `json:"max_eirp"`
                MaxPower int `json:"max_power"`
                MinPower int `json:"min_power"`
                MaxDataRate int `json:"max_datr"`
                InitialDutyCycle int `json:"dcycle_init"`
                InitialChannels string `json:"init_chans"`
                RXWIN_INIT struct{
                  RX1_DR_Offset int `json:"rx1_dr_offset"`
                  RX2_DR int `json:"rx2_dr"`
                  RX2_Freq float64 `json:"rx2_freq"`
                }`json:"rxwin_init"`
    					}{
    						Name:lorawan_serverNetworkName,
                NetID:"000001",
                Region:lorawan_serverNetworkRegion,
                CodingRate:"4/5",
                RX1JoinDelay:5,
                RX2JoinDelay:6,
                RX1Delay:1,
                RX2Delay:2,
                GatewayPower:16,
                MaxEIRP:16,
                MaxPower:0,
                MinPower:7,
                MaxDataRate:5,
                InitialDutyCycle:0,
                InitialChannels:"0-2",
                RXWIN_INIT:struct{
                  RX1_DR_Offset int `json:"rx1_dr_offset"`
                  RX2_DR int `json:"rx2_dr"`
                  RX2_Freq float64 `json:"rx2_freq"`
                }{
                  RX1_DR_Offset:0,
                  RX2_DR:2,
                  RX2_Freq:923.3,
                },
    					}
    var resp []Network
    err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/networks", "POST", req , &resp)
    if err != nil {
      //fmt.Println("Network err3",err) //delete debugging
      return err //delete debugging
		}
    if resp==nil{
      lorawan_server.NetworkName=lorawan_serverNetworkName
    }
  }
}
  return nil
}

func (lorawan_server *lorawan_server) initGroups()error{
  if lorawan_server.GroupName==""{
    type Group struct{
      Name string `json:"name"`
    }
    var respExist []Group

    err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/groups", "GET", nil, &respExist)
		if err != nil {
      //fmt.Println("Group err2",err) //delete debugging
  		return err //delete debugging
		}

    for _, orga := range respExist {
			if orga.Name == lorawan_serverGroupName { //if orga.Name matches Lorhammer
        lorawan_server.GroupName=orga.Name
        //fmt.Println("found->",lorawan_server.GroupName)
        break
			}
		}

    if lorawan_server.GroupName==""{
      req:= struct {
    						Name string `json:"name"`
                Network string `json:"network"`
    						//Add SubID for OTAA
                CanJoin bool `json:"can_join"`
    					}{
    						Name:lorawan_serverGroupName,
    						//ADD subId for OTAA
                Network:lorawan_server.NetworkName,
                CanJoin:true,
    					}
    var resp []Group
    err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/groups", "POST", req , &resp)
    if err != nil {
      //fmt.Println("Group err3",err) //delete debugging
      return err //delete debugging
		}
    if resp==nil{
      lorawan_server.GroupName=lorawan_serverGroupName
    }
  }
}
  return nil
}

func (lorawan_server *lorawan_server) initProfile()error{
  if lorawan_server.ProfileName==""{
    type Profile struct{
      Name string `json:"name"`
    }
    var respExist []Profile

    err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/profiles", "GET", nil, &respExist)
		if err != nil {
      //fmt.Println("Profile err2",err) //delete debugging
  		return err //delete debugging
		}

    for _, orga := range respExist {
			if orga.Name == lorawan_serverProfileName { //if orga.Name matches Lorhammer
        lorawan_server.ProfileName=orga.Name
        //fmt.Println("found->",lorawan_server.ProfileName)
        break
			}
		}

    if lorawan_server.ProfileName==""{
      req:= struct {
    						Name string `json:"name"`
                Group string `json:"group"`
    						Application string `json:"app"`
                //App Id if needed
                Join int `json:"join"`
                FCnt_Check int `json:"fcnt_check"` //refernce and confirm
                TX_Window int `json:"txwin"`
                ADR_Mode int `json:"adr_mode"` //disabled for now, refer the server api for enabling
                RequestStatus bool `json:"request_devstat"`
              }{
    						Name:lorawan_serverProfileName,
                Group:lorawan_server.GroupName,
                Application:"semtech-mote",
    						Join:1,
                FCnt_Check:2,
                TX_Window:0,
                ADR_Mode:0, //Change for supporting ADR, 0-disabled
    						RequestStatus:true,
    					}
    var resp []Profile
    err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/profiles", "POST", req , &resp)
    if err != nil {
      //fmt.Println("Profile err3",err) //delete debugging
      return err //delete debugging
		}
    if resp==nil{
      lorawan_server.ProfileName=lorawan_serverProfileName
    }
  }
}
  return nil
}

func (lorawan_server *lorawan_server) initGateway(macAddress lorawan.EUI64)error{
      type Gatewayy struct{
          MAC string `json:"mac"`
        }
      req:= struct {
    						MAC string `json:"mac"`
                Area string `json:"area"`
    						TX_Chain int `json:"tx_rfch"`
                AntennaGain int `json:"ant_gain"`
                Description string `json:"desc"`
    					}{
    						MAC:macAddress.String(),//change to model.Gateway EUI
                Area:lorawan_server.AreaName,
                TX_Chain:0,
                AntennaGain:6,//put suitable value after referencing
                Description:"lorhammer Gateway",
    					}
    var resp  []Gatewayy
		fmt.Println("Gateway Req%v",req)
    err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/gateways", "POST", req , &resp)
    if err != nil {
      //fmt.Println("Gateway err",err) //delete debugging
      //return err //delete debugging
			for 1>0{
				err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/gateways", "POST", req , &resp)
				if err==nil{
					break
				}
			}
		}
    if resp==nil{
      fmt.Println("Gateway created successfully")
    }
  return nil
}

func (lorawan_server *lorawan_server) provisionSensorAsync(sensorChan chan *model.Node, poison chan bool, errorChan chan error, sensorFinishChan chan *model.Node) {
	exit := false
	for {
		select {
		case sensor := <-sensorChan:
			if sensor != nil { // Why sensor is nil sometimes !?

        //abp activation
				if lorawan_server.Abp {
					type Nodee struct{
	          DevAddr string `json:"devAddr"`
	        }
					req:= struct {
						DevAddr string `json:"devaddr"`
						Profile string `json:"profile"`
						Location string `json:"location"`
						NwkSkey string `json:"nwkskey"`
						AppSkey string `json:"appskey"`
						Description string `json:"desc"`
						// FCnt_Up int `json:"fcntup"` //not needed
						// FCnt_Down int `json:"fcntdown"` //not needed
						//LastReset string `json:"last_reset"` //remove,
						//LastRX string `json:"last_rx"` //remove,
					}{
						DevAddr:sensor.DevAddr.String(),//change to model.Node EUI   sensor.DevEUI.String()
						Profile:lorawan_server.ProfileName,
						// Location:"SUTD, UpperChangi",
						NwkSkey:sensor.NwSKey.String(),//change to take from scenario
						AppSkey:sensor.AppSKey.String(),//change to take from scenario
						// Description:"Lorhammer STRESSNODE_" + sensor.DevEUI.String(),
						// FCnt_Up:0,//not needed
						// FCnt_Down:0,//not needed
						//LastReset:tm.String(),
						//LastRX:tm.String(),
					}

	         //registering the devices
					var resp  []Nodee
					err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/nodes", "POST", req , &resp)
					if err != nil {
						for true{
							err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/nodes", "POST", req , &resp)
							if err==nil{
								break
							}
						}
					}
				} else { // OTAA
					req := struct {
						DevEUI string `json:"deveui"`
						AppEUI string `json:"appeui"`
						Profile string `json:"profile"`
						AppKey string `json:"appkey"`
						NwkKey string `json:"nwkkey"`
					}{
						DevEUI: sensor.DevEUI.String(),
						AppEUI: sensor.AppEUI.String(),
						Profile:lorawan_server.ProfileName,
						AppKey: sensor.AppKey.String(),
						NwkKey: sensor.AppKey.String(),
					}
					
					err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/devices", "POST", req, nil)
					if err != nil {
						for true{
							err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/devices", "POST", req, nil)
							if err==nil{
								break
							}
						}
					}
				}
				sensorFinishChan <- sensor
			}
		case <-poison:
			exit = true
		}
		if exit {
			break
		}
	}
}

//Write DeProvisioner Code
//to deProvision
func (lorawan_server *lorawan_server) DeProvision() error {

	/*if lorawan_server.DeleteApplication && lorawan_server.AppID != "" {
		if err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/applications/"+lorawan_server.AppID, "DELETE", nil, nil); err != nil {
			return err
		}
	}

	if lorawan_server.DeleteOrganization && lorawan_server.OrganizationID != "" {
		if err := lorawan_server.doRequest(lorawan_server.APIURL+"/api/organizations/"+lorawan_server.OrganizationID, "DELETE", nil, nil); err != nil {
			return err
		}
	}*/

	return nil
}

//to have a http-request and returning the response
func (lorawan_server *lorawan_server) doRequest(url string, method string, bodyRequest interface{}, bodyResult interface{}) error {
	logLorawan_server.WithField("url", url).Debug("Will call") //logrus debugger
	ctx, cancelCtx := context.WithTimeout(context.Background(), 5*time.Second) //context is like a deadline
	defer cancelCtx()

	marshalledBodyRequest, err := json.Marshal(bodyRequest)  //converting req to json for Api server communication
	if err != nil {
		return err
	}

	req, err := http.NewRequest(method, url, bytes.NewBuffer(marshalledBodyRequest)) //creates a request compatible with client.Do
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	//req.Close = true  //Don't know for Concurrent in this case
	req.WithContext(ctx)

//Requesting the server and getting the responses
	resp, err := lorawan_server.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

//reading the body of the response
	body, _ := ioutil.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK: //For GET request
		logLorawan_server.WithField("url", url).Debug("Call succeeded")
	case http.StatusNoContent: //For POST request
		logLorawan_server.WithField("url", url).Debug("Call succeeded")
	default:
		logLorawan_server.WithFields(logrus.Fields{
			"respStatus":   resp.StatusCode,
			"responseBody": string(body),
			"requestBody":  string(marshalledBodyRequest),
			"url":          url,
		}).Warn("Couldn't proceed with request")
		return errors.New("Couldn't proceed with request")
	}

  //umarshalling the data from body and storing it in bodyResult and returning it
	if body != nil && bodyResult != nil {
		if resp.StatusCode==http.StatusOK{
		  return json.Unmarshal(body, bodyResult)
    }
	}
	return nil
}
