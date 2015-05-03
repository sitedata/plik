/**

    Plik upload server

The MIT License (MIT)

Copyright (c) <2015>
	- Mathieu Bodjikian <mathieu@bodjikian.fr>
	- Charles-Antoine Mathieu <skatkatt@root.gg>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
**/

package mongo

import (
	"crypto/tls"
	"github.com/root-gg/plik/server/common"
	"github.com/root-gg/utils"
	mgo "gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"net"
	"strconv"
	"time"
)

/*
 * User input is only safe in document field !!!
 * Keys with ( '.', '$', ... ) may be interpreted
 */

type MongoMetadataBackendConfig struct {
	Url        string
	Database   string
	Collection string
	Username   string
	Password   string
	Ssl        bool
}

func NewMongoMetadataBackendConfig(config map[string]interface{}) (this *MongoMetadataBackendConfig) {
	this = new(MongoMetadataBackendConfig)
	this.Url = "127.0.0.1:27017"
	this.Database = "plik"
	this.Collection = "meta"
	utils.Assign(this, config)
	return
}

type MongoMetadataBackend struct {
	config  *MongoMetadataBackendConfig
	session *mgo.Session
}

func NewMongoMetadataBackend(config map[string]interface{}) (this *MongoMetadataBackend) {
	this = new(MongoMetadataBackend)
	this.config = NewMongoMetadataBackendConfig(config)

	// Open connection
	dialInfo := &mgo.DialInfo{}
	dialInfo.Addrs = []string{this.config.Url}
	dialInfo.Database = this.config.Database
	if this.config.Username != "" && this.config.Password != "" {
		dialInfo.Username = this.config.Username
		dialInfo.Password = this.config.Password
	}
	if this.config.Ssl {
		dialInfo.DialServer = func(addr *mgo.ServerAddr) (net.Conn, error) {
			return tls.Dial("tcp", addr.String(), &tls.Config{InsecureSkipVerify: true})
		}
	}
	var err error
	this.session, err = mgo.DialWithInfo(dialInfo)
	if err != nil {
		common.Log().Fatalf("Unable to contact mongodb at %s : %s", this.config.Url, err.Error())
	}

	// Ensure everything is persisted and replicated
	this.session.SetMode(mgo.Strong, false)
	this.session.SetSafe(&mgo.Safe{})
	return
}

func (this *MongoMetadataBackend) Create(ctx *common.PlikContext, upload *common.Upload) (err error) {
	defer ctx.Finalize(err)
	session := this.session.Copy()
	defer session.Close()
	collection := session.DB(this.config.Database).C(this.config.Collection)
	err = collection.Insert(&upload)
	if err != nil {
		err = ctx.EWarningf("Unable to append metadata to mongodb : %s", err)
	}
	return
}

func (this *MongoMetadataBackend) Get(ctx *common.PlikContext, id string) (u *common.Upload, err error) {
	defer ctx.Finalize(err)
	session := this.session.Copy()
	defer session.Close()
	collection := session.DB(this.config.Database).C(this.config.Collection)
	u = &common.Upload{}
	err = collection.Find(bson.M{"id": id}).One(u)
	if err != nil {
		err = ctx.EWarningf("Unable to get metadata from mongodb : %s", err)
	}
	return
}

func (this *MongoMetadataBackend) AddOrUpdateFile(ctx *common.PlikContext, upload *common.Upload, file *common.File) (err error) {
	defer ctx.Finalize(err)
	session := this.session.Copy()
	defer session.Close()
	collection := session.DB(this.config.Database).C(this.config.Collection)
	err = collection.Update(bson.M{"id": upload.Id}, bson.M{"$set": bson.M{"files." + file.Id: file}})
	if err != nil {
		err = ctx.EWarningf("Unable to get metadata from mongodb : %s", err)
	}
	return
}

func (this *MongoMetadataBackend) RemoveFile(ctx *common.PlikContext, upload *common.Upload, file *common.File) (err error) {
	defer ctx.Finalize(err)
	session := this.session.Copy()
	defer session.Close()
	collection := session.DB(this.config.Database).C(this.config.Collection)
	err = collection.Update(bson.M{"id": upload.Id}, bson.M{"$unset": bson.M{"files." + file.Name: ""}})
	if err != nil {
		err = ctx.EWarningf("Unable to get remove file from mongodb : %s", err)
	}
	return
}

func (this *MongoMetadataBackend) Remove(ctx *common.PlikContext, upload *common.Upload) (err error) {
	defer ctx.Finalize(err)
	session := this.session.Copy()
	defer session.Close()
	collection := session.DB(this.config.Database).C(this.config.Collection)
	err = collection.Remove(bson.M{"id": upload.Id})
	if err != nil {
		err = ctx.EWarningf("Unable to get remove file from mongodb : %s", err)
	}
	return
}

func (this *MongoMetadataBackend) GetUploadsToRemove(ctx *common.PlikContext) (ids []string, err error) {
	defer ctx.Finalize(err)
	session := this.session.Copy()
	defer session.Close()
	collection := session.DB(this.config.Database).C(this.config.Collection)

	// Look for uploads older than MaxTTL to schedule them for removal
	ids = make([]string, 0)
	uploads := make([]*common.Upload, 0)
	b := bson.M{"$where": strconv.Itoa(int(time.Now().Unix())) + " > this.uploadDate+this.ttl"}

	err = collection.Find(b).All(&uploads)
	if err != nil {
		err = ctx.EWarningf("Unable to get uploads to remove : %s", err)
		return
	}

	// Append all ids to the toRemove list
	for _, upload := range uploads {
		ids = append(ids, upload.Id)
	}

	return
}
