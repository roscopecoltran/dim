package index

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/blevesearch/bleve"
	"github.com/blevesearch/bleve/search"
	"github.com/docker/distribution/digest"
	"github.com/docker/docker/reference"
	"github.com/docker/engine-api/types"
	"github.com/nhurel/dim/lib/registry"
	"github.com/nhurel/dim/lib/utils"
)

// Index manages indexation of docker images
type Index struct {
	// Index is the bleve.Index instance
	bleve.Index
	RegistryURL  string
	RegistryAuth *types.AuthConfig
	RegClient    registry.Client
}

type repoImage struct {
	repoName string
	image    *registry.Image
}

// New create a new instance to manage a index of a given registry into a specific directory
func New(dir string, registryURL string, registryAuth *types.AuthConfig) (*Index, error) {
	var i bleve.Index
	var reg registry.Client
	var err error

	mapping := bleve.NewIndexMapping()
	mapping.AddDocumentMapping("image", ImageMapping)
	if i, err = bleve.New(dir, mapping); err != nil {
		return nil, err
	}

	if reg, err = registry.New(registryAuth, registryURL); err != nil {
		return nil, err
	}

	return &Index{i, registryURL, registryAuth, reg}, nil
}

// Build creates a full index from the registry.
// The returned channel is closed once all images are indexed so the caller can block until the index is built if needed
func (idx *Index) Build() <-chan bool {
	done := make(chan bool, 1)

	go func() {
		repositories := idx.RegClient.WalkRepositories()

		images := make(chan *repoImage, 3)

		submitWg := sync.WaitGroup{}
		for repository := range repositories {
			submitWg.Add(1)
			go func(repo registry.Repository) {
				defer submitWg.Done()
				for img := range repo.WalkImages() {
					images <- &repoImage{repo.Named().Name(), img}
				}
			}(repository)
		}

		go func() {
			doneWG := sync.WaitGroup{}
			for img := range images {
				doneWG.Add(1)
				go func(n string, i *registry.Image) {
					defer doneWG.Done()
					idx.IndexImage(Parse(n, i))
				}(img.repoName, img.image)
			}
			doneWG.Wait()
			close(done)
		}()

		submitWg.Wait()
		close(images)
	}()
	return done
}

// GetImageAndIndex gets image details and updates the index
func (idx *Index) GetImageAndIndex(repository, tag string, dg digest.Digest) error {
	named, _ := reference.ParseNamed(repository)
	var repo registry.Repository
	var err error
	if repo, err = idx.RegClient.NewRepository(named); err != nil {
		logrus.WithError(err).WithField("Repository", repository).Errorln("Failed get repository info")
		return err
	}

	var img *registry.Image
	if img, err = repo.ImageFromManifest(dg, tag); err != nil {
		logrus.WithError(err).Errorln("Failed to get image info from manifest")
		return err
	}

	idx.IndexImage(Parse(repository, img))

	return nil
}

// IndexImage adds a given image into the index
func (idx *Index) IndexImage(image *Image) {
	logrus.WithFields(logrus.Fields{"imageID": image.ID, "image.FullName": image.FullName}).Debugln("Indexing image")
	idx.Index.Index(image.FullName, image)
}

// DeleteImage removes an image from the index
func (idx *Index) DeleteImage(id string) {
	l := logrus.WithField("imageID", id)
	l.Debugln("Removing image from index")
	q := bleve.NewTermQuery(id).SetField("ID")
	rq := bleve.NewSearchRequest(q)
	rq.Fields = []string{"FullName"}
	var sr *bleve.SearchResult
	var err error
	if sr, err = idx.Search(rq); err != nil || sr.Total == 0 {
		l.WithError(err).WithField("#hits", sr.Total).Errorln("Failed to get image id to remove from index")
		return
	}
	if sr.Total > 1 {
		l.WithField("#hits", sr.Total).Warnln("Removing multiple images from index for this imageID")
		return
	}

	for _, h := range sr.Hits {
		l.WithField("image.FullName", h.Fields["FullName"].(string)).Infoln("Removing image from index")
		idx.Index.Delete(h.Fields["FullName"].(string))
	}
}

// BuildQuery returns the query object corresponding to given parameters
func BuildQuery(nameTag, advanced string) bleve.Query {
	l := logrus.WithFields(logrus.Fields{"nameTag": nameTag, "advanced": advanced})
	l.Debugln("Building query clause")

	if nameTag == "*" || advanced == "*" {
		return bleve.NewMatchAllQuery()
	}

	bq := make([]bleve.Query, 0, 3)

	name := nameTag
	tag := nameTag

	if split := strings.Split(nameTag, ":"); len(split) == 2 {
		name = split[0]
		tag = split[1]
	}

	if nameTag != "" {
		l.WithFields(logrus.Fields{"name": name, "tag": tag}).Debugln("Adding name and tag clauses")
		bq = append(bq, bleve.NewFuzzyQuery(name).SetField("Name"), bleve.NewMatchQuery(tag).SetField("Tag"))
	}

	if advanced != "" {
		l.Debugln("Adding advanced clause")
		bq = append(bq, bleve.NewQueryStringQuery(advanced))
	}

	logrus.WithField("queries", bq).Debugln("Returning query with should clauses")
	return bleve.NewBooleanQuery(nil, bq, nil)

}

// SearchImages returns the images matching query.
// If fields is not empty, it fetches all given fields as well
func (idx *Index) SearchImages(q, a string, fields []string, offset, maxResults int) (*bleve.SearchResult, error) {
	var err error
	var sr *bleve.SearchResult
	request := bleve.NewSearchRequestOptions(BuildQuery(q, a), maxResults, offset, false)
	request.Fields = []string{"Name", "Tag", "FullName", "Labels", "Envs"}
	l := logrus.WithField("request", request).WithField("query", request.Query)
	l.Debugln("Running search")
	if sr, err = idx.Search(request); err != nil {
		return sr, fmt.Errorf("Error occured when processing search : %v", err)
	}

	if fields != nil && len(fields) > 0 {
		detailFields := make([]string, len(fields))
		copy(detailFields, fields)
		for _, f := range []string{"Name", "Tag", "FullName"} {
			if !utils.ListContains(detailFields, f) {
				detailFields = append(detailFields, f)
			}
		}

		for i, h := range sr.Hits {
			if sr.Hits[i], err = idx.searchDetails(h, detailFields); err != nil {
				return sr, fmt.Errorf("Error occured while searching details of an image : %x", err)
			}
		}
	}
	return sr, nil
}

func (idx *Index) searchDetails(doc *search.DocumentMatch, fields []string) (*search.DocumentMatch, error) {
	logrus.WithField("doc", doc).WithField("fields", fields).Debugln("Entering searchDetails")
	request := bleve.NewSearchRequest(bleve.NewDocIDQuery([]string{doc.ID}))
	request.Fields = fields
	if doc.Fields["Labels"] != nil && utils.ListContains(fields, "Labels") {
		switch f := doc.Fields["Labels"].(type) {
		case string:
			request.Fields = append(request.Fields, fmt.Sprintf("Label.%s", f))
		case []interface{}:
			for _, f := range doc.Fields["Labels"].([]interface{}) {
				request.Fields = append(request.Fields, fmt.Sprintf("Label.%s", f))
			}
		}
	}
	if doc.Fields["Envs"] != nil && utils.ListContains(fields, "Envs") {
		switch f := doc.Fields["Envs"].(type) {
		case string:
			request.Fields = append(request.Fields, fmt.Sprintf("Env.%s", f))
		case []interface{}:
			for _, f := range doc.Fields["Envs"].([]interface{}) {
				request.Fields = append(request.Fields, fmt.Sprintf("Env.%s", f))
			}
		}
	}

	var sr *bleve.SearchResult
	var err error
	if sr, err = idx.Search(request); err != nil {
		return nil, fmt.Errorf("Failed to fetch all image info : %v", err)
	}

	return sr.Hits[0], err
}
