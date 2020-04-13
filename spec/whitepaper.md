---
title: Corona Network
subtitle: Anonymous contact tracing and social distancing
date: April 13, 2020
author: Péter Szilágyi \<peterke@gmail.com\>

documentclass: scrartcl
header-includes:
  - \usepackage{draftwatermark}
---

*Abstract: The **Corona Network** is a decentralized system to aid in social distancing during the Coronavirus pandemic. It helps keeping an account of physical contacts and tracking suspected and confirmed infections across your friend circle. The goal is to act as an early warning system to self-isolate and to omit meeting potential carriers.*

*The Corona Network was designed from the ground up for **anonymity and privacy**. Data is stored exclusively on your mobile device and shared directly, end-to-end encrypted, with your approved contacts and events. Network connectivity is tumbled across the globe, anonymizing even your metadata. There is **no cloud, no server, no tracking!** Neither the authors, nor any other party can derive any data about you.*

*Source code is available for public scrutiny at https://github.com/coronanet. Note, the code is made available for **verification**. You are **not** granted a license to reuse it!*

# Introduction

TODO:

- Philosophy: decentralized vs. centrally orchestrated (GDPR, health data, data processing and storage laws, etc)
- Competing approaches: bluetooth and other wireless beacons (tracking vulnerability)

# Social network

TODO:

- Non-necessity of central coordination
- Eventual consistency data model

# Event graph

TODO:

- Events as spacial and temporal links between participants
- Participants and cross-time link between events
- Automatic infection status cascading

# Cryptography and identities

TODO:

- Permanent account identities and rotating discovery addresses
- Event identities and addresses, participant pseudonyms
 
# Networking and routing
 
TODO:

- Tor onion routing and tornet networking
- Servers, clients and P2P overlays

# Mobile application

TODO:

- rn-coronanet

# Disclaimer

TODO:

- Can only aid to detect infection risk, cannot prove non-risk
- Is deliberate anonymization good in this case?
