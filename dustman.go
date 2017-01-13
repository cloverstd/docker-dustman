package main

import (
	"gopkg.in/yaml.v2"
	dockerClient "github.com/docker/docker/client"
	dockerTypes "github.com/docker/docker/api/types"
	dockerFilters "github.com/docker/docker/api/types/filters"
	"context"
	"os"
	"io/ioutil"
	"log"
	"flag"
	"time"
	"github.com/robfig/cron"
)

type Config struct {
	DockerURI string `yaml:"docker_uri"`
	Label string `yaml:"label"`
	Crontab string `yaml:"crontab"`
	Image bool `yaml:"image"`
}

var config *Config


func getAbandonedContainer(label string, before int64) ([]dockerTypes.Container, error)  {
	client, err := dockerClient.NewClient(config.DockerURI, "v1.24", nil, nil)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	filters := dockerFilters.NewArgs()
	filters.Add("status", "exited")
	filters.Add("status", "dead")
	if len(label) > 0 {
		filters.Add("label", label)
	}
	options := dockerTypes.ContainerListOptions{
		All:true,
		Filters:filters,
	}
	_containers, err := client.ContainerList(context.Background(), options)
	if err != nil {
		return nil, err
	}
	if before == 0 {
		return _containers, nil
	}
	var containers []dockerTypes.Container
	for _, container := range _containers {
		if container.Created <= before {
			containers = append(containers, container)
		}
	}
	return containers, nil
}

func getDanglingImages() ([]dockerTypes.ImageSummary, error){
	client, err := dockerClient.NewClient(config.DockerURI, "v1.24", nil, nil)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	filters := dockerFilters.NewArgs()
	filters.Add("dangling", "true")
	options := dockerTypes.ImageListOptions{
		All:false,
		Filters:filters,
	}
	images, err := client.ImageList(context.Background(), options)
	return images, err
}

func initConfig(configFile string) error {
	if config == nil {
		if _, err := os.Stat(configFile); os.IsNotExist(err) {
			return err
		}
		data, err := ioutil.ReadFile(configFile)
		if err != nil {
			return err
		}
		err = yaml.Unmarshal([]byte(data), &config)
		if err != nil {
			return err

		}
	}
	return nil
}


func worker() {
	log.Println("start clean task")
	now := time.Now().Unix()
	defer func() {
		log.Printf("clean task done. spend %d s\n", time.Now().Unix()-now)
	}()
	client, err := dockerClient.NewClient(config.DockerURI, "v1.24", nil, nil)
	if err != nil {
		log.Println("get docker client failed. ", err)
		return
	}
	defer client.Close()
	containers, err := getAbandonedContainer(config.Label, now - 7200)
	if err != nil {
		log.Println("get abandoned container failed. ", err)
		return
	}
	containerOptions := dockerTypes.ContainerRemoveOptions{
		Force: false,
		RemoveLinks: false,
		RemoveVolumes: false,
	}
	log.Printf("%d containers wait to be clean\n", len(containers))
	for _, container := range containers {
		err = client.ContainerRemove(context.Background(), container.ID, containerOptions)
		if err != nil {
			log.Println("remove container failed. ", err)
		}
	}
	if config.Image == true {
		images, err := getDanglingImages()
		if err != nil {
			log.Println("get dangling images failed. ", err)
			return
		}
		imageOptions := dockerTypes.ImageRemoveOptions{
			Force:false,
			PruneChildren:false,
		}
		log.Printf("%d images wait to be clean\n", len(containers))
		for _, image := range images {
			_, err := client.ImageRemove(context.Background(), image.ID, imageOptions)
			if err != nil {
				log.Println("remove image failed. ", err)
			}
		}
	}
}

func main() {
	var configFile = flag.String("config", "dustman.yaml", "config file")
	if err := initConfig(*configFile); err != nil {
		log.Fatalf("load config failed. %s\n", err)
	}
	if len(config.Crontab) == 0 {
		log.Fatalln("crantab is required")
	}
	c := cron.New()
	defer c.Stop()
	if err := c.AddFunc(config.Crontab, worker); err != nil {
		log.Fatalf("add work to crontab@%s failed.\n", config.Crontab)
	}

	c.Start()
	select {}
}