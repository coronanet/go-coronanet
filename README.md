### How to build

The Corona Network protocol is written in [Go](https://golang.org/). If you want to contribute to this part of the code, you need to have a valid Go installation. We are also using [Go modules](https://blog.golang.org/using-go-modules), please familiarize yourself with them if they are new to you. You will also need a C compiler as certain dependencies of this project are in C.

If you can run `go install` in the repo root successfully, you're halfway there!

Go is one prerequisite, but it's not the only one. The main platform we are aiming for are mobile phones, so this project also needs to compile to them. We use [`gomobile`](https://github.com/golang/mobile/) to create the library archives that can be imported into mobile projects. You do not need to be familiar with `gomobile`, but you need to be able to run it.

You can install `gomobile` via:

```
$ go get -u golang.org/x/mobile/cmd/gomobile
$ go get -u golang.org/x/mobile/cmd/gobind
```

#### Android

To build the Android library archive (`.aar`), you need to have an Android SDK and NDK installed and the `ANDROID_HOME` environment var correctly set. Please consult the Android docs if you're stuck. You might want to do it through [Android Studio](https://developer.android.com/studio). We're not going to use the Android Studio at all, but it's an easy way to manage your SDKs and Android emulators.

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

#### iOS

iOS is not planned for the initial MVP to keep the scope smaller. A lot of prerequisite work needs to be done on supporting infra first (`libtor`, `ghostbridge`, etc), which is wasted time until it's proven to be worth it.
