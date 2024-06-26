/*
 * (C)  2019-present Alibaba Group Holding Limited.
 *
 * This program is free software; you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 2 as
 * published by the Free Software Foundation.
 */

/**
 * @author : Liu Jianping
 * @date : 2019/11/20
 */

package gdbclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/deverz/alibabacloud-gdb-go-sdk/gdbclient/graph"
	"github.com/deverz/alibabacloud-gdb-go-sdk/gdbclient/ilog"
	"github.com/deverz/alibabacloud-gdb-go-sdk/gdbclient/internal"
	"github.com/deverz/alibabacloud-gdb-go-sdk/gdbclient/internal/graphsonv3"
	"github.com/deverz/alibabacloud-gdb-go-sdk/gdbclient/internal/pool"
	"go.uber.org/zap"
	"strconv"
	"time"
	"unsafe"
)

func SetMyLogger(logger ilog.ILogger) {
	if logger == nil {
		return
	}
	internal.Logger = logger
}

func SetLogger(logger *zap.Logger) {
	if logger == nil {
		return
	}
	internal.Logger = internal.NewDefaultLogger(logger)
}

//---------------------- Gdb baseClient ---------------------//

// transaction ops
const (
	_OPEN     = "g.tx().open()"
	_COMMIT   = "g.tx().commit()"
	_ROLLBACK = "g.tx().rollback()"
)

// client shell for submit serial API
type ClientShell interface {
	SubmitScript(gremlin string) ([]Result, error)
	SubmitScriptBound(gremlin string, bindings map[string]interface{}) ([]Result, error)
	SubmitScriptOptions(gremlin string, options *graph.RequestOptions) ([]Result, error)

	SubmitScriptAsync(gremlin string) (ResultSetFuture, error)
	SubmitScriptBoundAsync(gremlin string, bindings map[string]interface{}) (ResultSetFuture, error)
	SubmitScriptOptionsAsync(gremlin string, options *graph.RequestOptions) (ResultSetFuture, error)
}

type SetLogContext interface {
	WithLogContext(ctx context.Context) *baseClient
}

// session client support batch submit
type SessionClient interface {
	BatchSubmit(func(ClientShell) error) error
	SetLogContext
	Close()
}

// session-less client support submit in sync or async, all in auto-transaction
type Client interface {
	ClientShell
	SetLogContext
	Close()
}

type baseClient struct {
	setting   *Settings
	sessionId string
	session   bool
	connPool  *pool.ConnPool
}

func NewClient(settings *Settings) Client {
	settings.init()
	client := &baseClient{setting: settings, session: false}
	client.connPool = pool.NewConnPool(settings.getOpts())
	internal.Logger.Info("new client", zap.String("server", client.String()), zap.Bool("session", client.session), zap.Time("createTime", time.Now()))
	return client
}

func NewSessionClient(sessionId string, settings *Settings) SessionClient {
	settings.init()
	client := &baseClient{setting: settings, session: true, sessionId: sessionId}
	client.connPool = pool.NewConnPool(settings.getSessionOpts())
	internal.Logger.Info("new client", zap.String("server", client.String()), zap.Bool("session", client.session), zap.Time("createTime", time.Now()))
	return client
}

func (c *baseClient) WithLogContext(ctx context.Context) *baseClient {
	internal.Logger = internal.Logger.WithContext(ctx)
	return c
}

func (c *baseClient) String() string {
	return fmt.Sprintf("Gdb<%s>", c.getEndpoint())
}

func (c *baseClient) Close() {
	if c.session {
		c.closeSession()
	}
	c.connPool.Close()
	internal.Logger.Info("close client", zap.Bool("session", c.session), zap.Time("time", time.Now()))
}

func (c *baseClient) getEndpoint() string {
	return c.setting.Host + ":" + strconv.FormatInt(int64(c.setting.Port), 10)
}

func (c *baseClient) SubmitScript(gremlin string) ([]Result, error) {
	return c.SubmitScriptBound(gremlin, nil)
}

func (c *baseClient) SubmitScriptBound(gremlin string, bindings map[string]interface{}) ([]Result, error) {
	options := graph.NewRequestOptionsWithBindings(bindings)
	return c.SubmitScriptOptions(gremlin, options)
}

func (c *baseClient) SubmitScriptOptions(gremlin string, options *graph.RequestOptions) ([]Result, error) {
	if future, err := c.SubmitScriptOptionsAsync(gremlin, options); err != nil {
		return nil, err
	} else {
		return future.GetResults()
	}
}

func (c *baseClient) SubmitScriptAsync(gremlin string) (ResultSetFuture, error) {
	return c.SubmitScriptBoundAsync(gremlin, nil)
}

func (c *baseClient) SubmitScriptBoundAsync(gremlin string, bindings map[string]interface{}) (ResultSetFuture, error) {
	options := graph.NewRequestOptionsWithBindings(bindings)
	return c.SubmitScriptOptionsAsync(gremlin, options)
}

func (c *baseClient) SubmitScriptOptionsAsync(gremlin string, options *graph.RequestOptions) (ResultSetFuture, error) {
	// set session args if session mode
	if c.session {
		if options == nil {
			options = graph.NewRequestOptionsWithBindings(nil)
		}
		options.AddArgs(graph.ARGS_SESSION, c.sessionId)
		options.AddArgs(graph.ARGS_MANAGE_TRANSACTION, c.setting.IsManageTransaction)
	}

	request, err := graphsonv3.MakeRequestWithOptions(gremlin, options)
	if err != nil {
		return nil, err
	}

	respFuture, err := c.requestAsync(request)
	if err != nil {
		return nil, err
	}
	return NewResultSetFuture(respFuture), nil
}

// session batch submit with 'SubmitScript' serial , must check return errors
func (c *baseClient) BatchSubmit(batchSubmit func(ClientShell) error) error {
	if !c.session {
		return errors.New("batch submit is not allowed in non-session client")
	}

	if err := c.transaction(_OPEN); err != nil {
		return err
	}

	err := batchSubmit(c)
	if err == nil {
		err = c.transaction(_COMMIT)
	}

	// rollback submit errors, include batch submit and commit
	if err != nil {
		err2 := c.transaction(_ROLLBACK)
		if err2 != nil {
			internal.Logger.Error("unstable transaction status as rollback failed", zap.Error(err), zap.Time("time", time.Now()))
			return err2
		}
	}
	return err
}

func (c *baseClient) closeSession() {
	request := graphsonv3.MakeRequestCloseSession(c.sessionId)
	respFuture, err := c.requestAsync(request)
	if err != nil {
		internal.Logger.Warn("fail to close session", zap.Error(err), zap.Time("time", time.Now()))
		return
	}

	// NOTICE: wait to get response of session close request
	if resp, timeout := respFuture.GetOrTimeout(2 * time.Second); timeout {
		internal.Logger.Warn("response timeout for close session", zap.Time("time", time.Now()))
	} else {
		if resp.Code != graphsonv3.RESPONSE_STATUS_NO_CONTENT && resp.Code != graphsonv3.RESPONSE_STATUS_SUCCESS {
			internal.Logger.Warn("response error for close session", zap.Error(resp.Data.(error)), zap.Time("time", time.Now()))
		}
	}
}

func (c *baseClient) transaction(ops string) error {
	options := graph.NewRequestOptionsWithBindings(nil)
	options.AddArgs(graph.ARGS_SESSION, c.sessionId)
	options.AddArgs(graph.ARGS_MANAGE_TRANSACTION, c.setting.IsManageTransaction)

	request, err := graphsonv3.MakeRequestWithOptions(ops, options)
	if err != nil {
		return err
	}

	respFuture, err := c.requestAsync(request)
	if err != nil {
		return err
	}

	// just check response code instead of un-json Data, transaction return 'null'...
	resp := respFuture.Get()
	if err, ok := resp.Data.(error); ok {
		return err
	}
	return nil
}

func (c *baseClient) requestAsync(request *graphsonv3.Request) (*graphsonv3.ResponseFuture, error) {
	conn, err := c.connPool.Get()
	if err != nil {
		internal.Logger.Error("request connect failed",
			zap.Time("time", time.Now()),
			zap.Error(err))
		return nil, err
	}

	bindingsStr, _ := json.Marshal(request.Args[graph.ARGS_BINDINGS])
	// send request to connection, and return future
	internal.Logger.Info("submit script",
		zap.Time("time", time.Now()),
		zap.Uintptr("conn", uintptr(unsafe.Pointer(conn))),
		zap.String("dsl", request.Args[graph.ARGS_GREMLIN].(string)),
		zap.String("bindings", string(bindingsStr)),
		zap.String("processor", request.Processor))

	f, err := conn.SubmitRequestAsync(request)
	if err != nil {
		// return connection to pool if request is not pending
		c.connPool.Put(conn)
		internal.Logger.Warn("submit script failed",
			zap.Time("time", time.Now()),
			zap.Uintptr("conn", uintptr(unsafe.Pointer(conn))),
			zap.Error(err),
			zap.String("dsl", request.Args[graph.ARGS_GREMLIN].(string)))
	}
	return f, err
}
