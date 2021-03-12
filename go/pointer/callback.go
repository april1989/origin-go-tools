package pointer

/*
bz: input is callback.yml
    analyze yml file -> users specify what is the callback function
           they want us to analyze.

 */


type CallBack struct {
	Packages []Package
}

type Package struct {
	Methods  []Method
}

type Method struct {
	Name     string
	Receiver string
}

//bz: decode yml file, absolute path -> where is the callback.yml
func DecodeYaml(path string)  {


}