:exclamation: *This project is currently in the phase of figuring out the absolute minimum requirements to be useful; and creating a feasibility study (live networking code) to support those requirements. When the networking seems usable, we'll go for a interfaceable proof-of-concept, but not before. There's a concurrent mock UI experiment in [`rn-coronanet`](https://github.com/coronanet/rn-coronanet) for the frontend side of things, which also blocks the PoC work to avoid wasting time.* :exclamation:

---

## Interfacing

The Corona Network is a decentralized peer-to-peer social network. Instead of a cloud or backend server, this library itself is providing the APIs that thin clients (i.e. mobile user interfaces) rely on. This is done via hosting a local HTTP server that substitutes *"the cloud"*, in reality being a gateway into a distributed world.

For a long rundown behind the model with regard to rationales, privacy concerns and security concerns, please see the [`go-ghostbridge`](https://github.com/ipsn/go-ghostbridge) project. As a quick TL;DR:

* *Emulating* a cloud through a REST API permits us to naturally build a React Native interface on top (see [`rn-coronanet`](https://github.com/coronanet/rn-coronanet)). Crossing over from React Native to Java to C to Go via language bindings is not sustainable.
* Securing the data traffic between the React Native user interface and the REST API server is done through HTTPS via ephemeral server side SSL certificates injected directly into the client on startup.
* Securing the API server from unauthenticated access is done through ephemeral API tokens injected directly into the client on startup.

You can check the latest version of the API spec [through Swagger](https://editor.swagger.io/?url=https://raw.githubusercontent.com/coronanet/go-coronanet/master/spec/api.yaml).

## How to build

The Corona Network protocol is written in [Go](https://golang.org/). If you want to contribute to this part of the code, you need to have a valid Go installation. We are also using [Go modules](https://blog.golang.org/using-go-modules), please familiarize yourself with them if they are new to you. You will also need a C compiler as certain dependencies of this project are in C.

If you can run `go install` in the repo root successfully, you're halfway there!

Go is one prerequisite, but it's not the only one. The main platform we are aiming for are mobile phones, so this project also needs to compile to them. We use [`gomobile`](https://github.com/golang/mobile/) to create the library archives that can be imported into mobile projects. You do not need to be familiar with `gomobile`, but you need to be able to run it.

You can install `gomobile` via:

```
$ go get -u golang.org/x/mobile/cmd/gomobile
$ go get -u golang.org/x/mobile/cmd/gobind
```

### Android

To build the Android library archive (`.aar`), you need to have an Android SDK and NDK installed and the `ANDROID_HOME` environment variable correctly set. Please consult the Android docs if you're stuck. You might want to do it through [Android Studio](https://developer.android.com/studio). We're not going to use the Android Studio at all, but it's an easy way to manage your SDKs and Android emulators.

Once Android is configured, you can build `go-coronanet` via:

```
$ gomobile bind --target android --javapkg xyz -v github.com/coronanet/go-coronanet
```

The first time you do the above, it will take a **LOT** of time. I'm not kidding, on the order of **30 minutes**, as it needs to build some humongous C dependencies for 4 different architectures (x86, x86_64, arm, arm64). The good news is that subsequent builds will be fast(er).

```
$ ls -al | grep coronanet

-rw-r--r-- 1 karalabe karalabe 46961891 Mar 16 19:19 coronanet.aar
-rw-r--r-- 1 karalabe karalabe     6383 Mar 16 19:19 coronanet-sources.jar
```

Whoa, that final binary size is insane. Yes it is, but it does contain 4 architectures + debug symbols. Long term a proper build system could make things a bit more pleasant. Optimizing app size is not relevant at this phase, simplicity and portability are more useful.

### iOS

iOS is not planned for the initial MVP to keep the scope smaller. A lot of prerequisite work needs to be done on supporting infra first ([`go-libtor`](https://github.com/ipsn/go-libtor), [`go-ghostbridge`](https://github.com/ipsn/go-ghostbridge), etc), which is wasted time until it's proven to be worth it.

## Contributing

This project is an experiment.

I'm very grateful for any and all contributions, but you must be aware that there are yet-unsolved challenges around running a decentralized social network. There's a fair probability that the project will flop, invest your time accordingly.

The goal of the Corona Network is to be a tiny, *use-case specific decentralized social network with privacy and security* above all else. If it cannot be done within these constraints, it won't be done. **No cloud, no server, no tracking.**

## License

I don't know. This project contains a lot of my free time and a lot of my past ideas and work distilled down. I'm happy to give it all away for making the world a nicer place, but I am not willing to accept anyone making money off of it. Open to suggestions.

Until the above is figured out, contributors agree to grant their code to me ([@karalabe](https://github.com/karalabe)).
