/*
 * Copyright 2018-2019, CS Systemes d'Information, http://www.c-s.fr
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package listeners

import (
	"context"
	google_protobuf "github.com/golang/protobuf/ptypes/empty"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/CS-SI/SafeScale/lib"
	"github.com/CS-SI/SafeScale/lib/server/handlers"
	"github.com/CS-SI/SafeScale/lib/server/utils"
	conv "github.com/CS-SI/SafeScale/lib/server/utils"
)

// DataHandler ...
var DataHandler = handlers.NewDataHandler

// DataListener is the data service grpc server
type DataListener struct{}

// List will returns all the files from one or several ObjectStorages
func (s *DataListener) List(ctx context.Context, in *google_protobuf.Empty) (*pb.FileList, error) {
	// defer timing.TimerWithLevel(fmt.Sprintf("listeners.DataListener::List() called"), log.TraceLevel)()

	ctx, cancelFunc := context.WithCancel(ctx)

	if err := utils.ProcessRegister(ctx, cancelFunc, "Data List"); err == nil {
		defer utils.ProcessDeregister(ctx)
	}

	tenants := GetCurrentStorageTenants()
	if tenants == nil {
		log.Info("Can't list buckets: no storage tenants set")
		return nil, status.Errorf(codes.FailedPrecondition, "can't list buckets: no storage tenants set")
	}

	handler := DataHandler(tenants.StorageServices)
	fileNames, uploadDates, fileSizes, fileBuckets, err := handler.List(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return conv.ToPBFileList(fileNames, uploadDates, fileSizes, fileBuckets), nil
}

// Push upload a file to one or several ObjectStorages
func (s *DataListener) Push(ctx context.Context, in *pb.File) (*google_protobuf.Empty, error) {
	// defer timing.TimerWithLevel(fmt.Sprintf("listeners.DataListener::Push(%s) called", in.GetLocalPath()), log.TraceLevel)()

	ctx, cancelFunc := context.WithCancel(ctx)

	if err := utils.ProcessRegister(ctx, cancelFunc, "Data Push"); err == nil {
		defer utils.ProcessDeregister(ctx)
	}

	tenants := GetCurrentStorageTenants()
	if tenants == nil {
		log.Info("Can't list buckets: no storage tenants set")
		return nil, status.Errorf(codes.FailedPrecondition, "can't list buckets: no storage tenants set")
	}

	handler := DataHandler(tenants.StorageServices)
	err := handler.Push(ctx, in.GetLocalPath(), in.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &google_protobuf.Empty{}, nil
}

// Get fetch a file from one or several ObjectStorages
func (s *DataListener) Get(ctx context.Context, in *pb.File) (*google_protobuf.Empty, error) {
	// defer timing.TimerWithLevel(fmt.Sprintf("listeners.DataListener::Get(%s) called", in.GetName()), log.TraceLevel)()

	ctx, cancelFunc := context.WithCancel(ctx)

	if err := utils.ProcessRegister(ctx, cancelFunc, "Data Push"); err == nil {
		defer utils.ProcessDeregister(ctx)
	}

	tenants := GetCurrentStorageTenants()
	if tenants == nil {
		log.Info("Can't list buckets: no storage tenants set")
		return nil, status.Errorf(codes.FailedPrecondition, "can't list buckets: no storage tenants set")
	}

	handler := DataHandler(tenants.StorageServices)
	err := handler.Get(ctx, in.GetLocalPath(), in.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &google_protobuf.Empty{}, nil
}

// Delete remove a file from one or several ObjectStorages
func (s *DataListener) Delete(ctx context.Context, in *pb.File) (*google_protobuf.Empty, error) {
	// defer timing.TimerWithLevel(fmt.Sprintf("listeners.DataListener::Delete(%s) called", in.GetName()), log.TraceLevel)()

	ctx, cancelFunc := context.WithCancel(ctx)

	if err := utils.ProcessRegister(ctx, cancelFunc, "Data Delete"); err == nil {
		defer utils.ProcessDeregister(ctx)
	}

	tenants := GetCurrentStorageTenants()
	if tenants == nil {
		log.Info("Can't list buckets: no storage tenants set")
		return nil, status.Errorf(codes.FailedPrecondition, "can't list buckets: no storage tenants set")
	}

	handler := DataHandler(tenants.StorageServices)
	err := handler.Delete(ctx, in.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	return &google_protobuf.Empty{}, nil
}
