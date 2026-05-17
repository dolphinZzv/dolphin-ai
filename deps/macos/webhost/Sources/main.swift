import Foundation
import NIO
import NIOHTTP1
import Logging

let logger = Logger(label: "com.dolphin.webhost")
let group = MultiThreadedEventLoopGroup(numberOfThreads: System.coreCount)
let server = McpServer(eventLoopGroup: group)

let bootstrap = ServerBootstrap(group: group)
bootstrap.serverChannelOption(ChannelOptions.backlog, value: 128)
bootstrap.serverChannelOption(ChannelOptions.reuseAddress, value: true)
bootstrap.childChannelOption(ChannelOptions.reuseAddress, value: true)
bootstrap.childChannelOption(ChannelOptions.maximumFrameSize, value: 65536)

_ = bootstrap.pipelineHandlers { channel in
    channel.pipeline.addHTTPServerHandlers()
        .flatMap { channel.pipeline.addHandler(HttpPipelineHandler(server: server)) }
}

let channel = try bootstrap.bind(host: "localhost", port: 9223).wait()

logger.info("WebHost started on localhost:9223")

try channel.closeFuture.wait()
try group.syncShutdownGracefully()