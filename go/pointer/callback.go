package pointer

import (
	"fmt"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
)

/*
bz: input is callback.yml
    analyze yml file -> users specify what is the callback function
           they want us to analyze.

 */

var (
	debugCB = true //debug
	Signatures = make([]string, 0) //record of all pkg + method signatures that we have to analyze
)

type CallBack struct {
	CallBackCfgs []CallBackCfg `yaml:"callbackcfgs"`
}

func (cb *CallBack) String() string {
	r := ""
	for _, cfg := range cb.CallBackCfgs {
		r = r + "Package: " + cfg.Package + "\n"
		for _, m := range cfg.Methods {
			r = r + " - Method: " + m.Name + ", Receiver: " + m.Receiver + "\n"
		}
	}
	return r
}

type CallBackCfg struct {
	Package string `yaml:"package"`
	Methods  []Method `yaml:"method"`
}

type Method struct {
	Name     string `yaml:"name"`
	Receiver string `yaml:"receiver"`
}

//bz: decode yml file, path -> absolute path, where is the callback.yml
func DecodeYaml(path string)  {
	cbfile, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	cbs := CallBack{}
	err = yaml.Unmarshal(cbfile, &cbs)
	if err != nil {
		log.Fatalf("Yml Decode Error: %v", err)
	}

	if debugCB {
		fmt.Println("------------------------------\nDump callback.yml: \n",
			cbs.String(), "------------------------------")
	}

	//assemble to the string format we used in pointer pkgs
	for _, cfg := range cbs.CallBackCfgs {
		pkg := cfg.Package //Package
		for _, m := range cfg.Methods {
			fn := m.Name
			receiver := m.Receiver
			var sig string
			if receiver == "nil" { //static
				sig = pkg + "." + fn
			}else{ //virtual
				sig = "(*" + receiver + ")." + fn
			}
			Signatures = append(Signatures, sig)
		}
	}

	if debugCB {
		fmt.Println("------------------------------\nDump Signatures: (#", len(Signatures), ")")
		for i, sig := range Signatures {
			fmt.Println(i, ". ", sig)
		}
		fmt.Println("------------------------------")
	}
}

