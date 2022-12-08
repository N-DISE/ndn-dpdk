import type { ActivateFwArgs, ActivateGenArgs, EtherLocator, FaceLocator, FetchCounters, FetchTaskDef, FileServerConfig, TgpConfig, VxlanLocator } from "@usnistgov/ndn-dpdk";
import delay from "delay";
import assert from "minimalistic-assert";

import { GqlFwControl, GqlGenControl } from "./control";
import { hexPad, uniqueRandomVector } from "./util";

export interface ServerEnv {
  F_GQLSERVER: string;
  F_PORT_A: string;
  F_PORT_B: string;
  F_NUMA_PRIMARY: number;
  F_CORES_PRIMARY: number[];
  F_CORES_SECONDARY: number[];
  A_GQLSERVER: string;
  A_PORT_F: string;
  A_NUMA_PRIMARY: number;
  A_CORES_PRIMARY: number[];
  A_CORES_SECONDARY: number[];
  A_FILESERVER_PATH: string;
  B_GQLSERVER: string;
  B_PORT_F: string;
  B_NUMA_PRIMARY: number;
  B_CORES_PRIMARY: number[];
  B_CORES_SECONDARY: number[];
  B_FILESERVER_PATH: string;
}

export interface BenchmarkOptions {
  faceAScheme: BenchmarkOptions.FaceScheme;
  faceARxQueues: number;
  faceBScheme: BenchmarkOptions.FaceScheme;
  faceBRxQueues: number;
  nFwds: number;
  trafficDir: BenchmarkOptions.TrafficDir;
  producerKind: BenchmarkOptions.ProducerKind;
  nProducerThreads: number;
  nFlows: number;
  interestNameLen: number;
  dataMatch: BenchmarkOptions.DataMatch;
  payloadLen: number;
  segmentEnd: number;
  warmup: number;
  duration: number;
}
export namespace BenchmarkOptions {
  export type FaceScheme = "ether" | "vxlan" | "memif";
  export type TrafficDir = 2 | 1;
  export type ProducerKind = "pingserver" | "fileserver";
  export type DataMatch = "exact" | "prefix";
}

export interface BenchmarkResult {
  duration: number;
  pps: number;
  bps: number;
}

export class Benchmark {
  constructor(
      private readonly env: ServerEnv,
      private readonly opts: BenchmarkOptions,
      signal: AbortSignal,
  ) {
    this.cF = new GqlFwControl(env.F_GQLSERVER);
    this.cA = new GqlGenControl(env.A_GQLSERVER);
    this.cB = env.A_GQLSERVER === env.B_GQLSERVER ? this.cA : new GqlGenControl(env.B_GQLSERVER);
    signal.addEventListener("abort", () => {
      this.cF.close();
      this.cA.close();
      this.cB.close();
    });
  }

  private readonly cF: GqlFwControl;
  private readonly cA: GqlGenControl;
  private readonly cB: GqlGenControl;
  private state = makeInitialState();

  public async setup(): Promise<void> {
    await Promise.all([
      this.activateForwarder(),
      this.activateTrafficGen("A"),
      this.cA === this.cB ? undefined : this.activateTrafficGen("B"),
    ]);
    await Promise.all(tgNodeLabels.map((label) => this.startTrafficGen(label)));
  }

  private async activateForwarder(): Promise<void> {
    await this.cF.restart();

    const {
      faceARxQueues,
      faceBRxQueues,
      nFwds,
    } = this.opts;
    const alloc = this.env.F_CORES_PRIMARY.concat();
    const arg: ActivateFwArgs = {
      eal: {
        cores: [...this.env.F_CORES_PRIMARY, ...this.env.F_CORES_SECONDARY],
        lcoreMain: this.env.F_CORES_SECONDARY[0],
      },
      lcoreAlloc: {
        RX: alloc.splice(0, faceARxQueues + faceBRxQueues),
        TX: alloc.splice(0, 2),
        FWD: alloc.splice(0, nFwds),
        CRYPTO: [this.env.F_CORES_SECONDARY[1]],
      },
      mempool: {
        DIRECT: { capacity: 1048575, dataroom: 9146 },
        INDIRECT: { capacity: 2097151 },
      },
      ndt: { prefixLen: 2 },
      fib: { startDepth: 4 },
      pcct: {
        pcctCapacity: 65535,
        csMemoryCapacity: 4096,
        csIndirectCapacity: 4096,
      },
      fwdInterestQueue: { dequeueBurstSize: 32 },
      fwdDataQueue: { dequeueBurstSize: 64 },
      fwdNackQueue: { dequeueBurstSize: 64 },
    };
    await this.cF.activate("forwarder", arg);

    const seenNdtIndices = new Set<number>();
    for (const label of tgNodeLabels) {
      const face = await this.cF.createFace(await this.prepareLocator(this.cF, this.env[`F_PORT_${label}`], label));
      this.state.face[label] = face;

      for (let j = 0; j < nFwds; ++j) {
        const name = `/${label}/${j}`;
        await this.cF.insertFibEntry(name, face);

        const index = await this.cF.updateNdt(name, j % nFwds);
        this.state.ndtDuplicate ||= seenNdtIndices.has(index);
        seenNdtIndices.add(index);
      }
    }
  }

  private async activateTrafficGen(label: TgNodeLabel): Promise<void> {
    const ctrl = this[`c${label}`];
    await ctrl.restart();

    const arg: ActivateGenArgs = {
      eal: {
        cores: [...this.env[`${label}_CORES_PRIMARY`], ...this.env[`${label}_CORES_SECONDARY`]],
        lcoreMain: this.env[`${label}_CORES_SECONDARY`][0],
      },
      mempool: {
        DIRECT: { capacity: 65535, dataroom: 9146 },
        INDIRECT: { capacity: 1048575 },
        PAYLOAD: { capacity: 16383 },
      },
    };
    await ctrl.activate("trafficgen", arg);
  }

  private async startTrafficGen(label: TgNodeLabel): Promise<void> {
    const ctrl = this[`c${label}`];
    const locator = await this.prepareLocator(ctrl, this.env[`${label}_PORT_F`], label);
    const result = await ctrl.startTrafficGen({
      face: locator,
      ...this.makeProducerConfig(label),
      fetcher: {
        nThreads: 1,
        nTasks: this.opts.nFlows,
      },
    });
    this.state.fetcher[label] = result.fetcher!;
    this.state.fileServerVersionBypassHi[label] = BigInt(result.fileServerVersionBypassHi ?? 0);
  }

  private async prepareLocator(ctrl: GqlFwControl | GqlGenControl, portVlan: string, faceLabel: TgNodeLabel): Promise<FaceLocator> {
    const isForwarder = ctrl === this.cF;
    const scheme = this.opts[`face${faceLabel}Scheme`];
    if (scheme === "memif") {
      return {
        scheme: "memif",
        role: isForwarder ? "server" : "client",
        socketName: "/run/ndn/ndndpdk-benchmark-memif.sock",
        id: faceLabel.codePointAt(0)!,
        dataroom: 9000,
      };
    }

    const [pciAddr, vlan] = portVlan.split("+");
    const port = await ctrl.createEthPort(pciAddr);
    const nRxQueues = this.opts[`face${faceLabel}RxQueues`];
    const macAddrLastOctet = hexPad(faceLabel.codePointAt(0)!, 2);
    return {
      port,
      nRxQueues,
      local: `02:00:00:00:${isForwarder ? "00" : "01"}:${macAddrLastOctet}`,
      remote: `02:00:00:00:${isForwarder ? "01" : "00"}:${macAddrLastOctet}`,
      vlan: vlan === undefined ? undefined : Number.parseInt(vlan, 10),
      ...(scheme === "vxlan" ? vxlanLocatorFields : { scheme: "ether" }),
    };
  }

  private makeProducerConfig(label: string): { producer?: TgpConfig; fileServer?: FileServerConfig } {
    const {
      nFwds,
      trafficDir,
      producerKind,
      nProducerThreads,
      dataMatch,
      payloadLen,
    } = this.opts;

    if (!trafficDirProducers[trafficDir].includes(label as any)) {
      return {};
    }

    switch (producerKind) {
      case "pingserver": {
        const producer: TgpConfig = {
          nThreads: nProducerThreads,
          patterns: [],
        };
        for (let j = 0; j < nFwds; ++j) {
          producer.patterns.push({
            prefix: `/${label}/${j}`,
            replies: [{
              suffix: dataMatch === "exact" ? undefined : "/D",
              payloadLen,
              freshnessPeriod: 1,
            }],
          });
        }
        return { producer };
      }
      case "fileserver": {
        const fileServer: FileServerConfig = {
          nThreads: nProducerThreads,
          mounts: [{
            prefix: `/${label}`,
            path: this.env[`${label}_FILESERVER_PATH`],
          }],
          segmentLen: payloadLen,
          wantVersionBypass: true,
        };
        return { fileServer };
      }
    }
  }

  public async run(): Promise<BenchmarkResult> {
    const {
      payloadLen,
      warmup,
      duration,
    } = this.opts;

    await this.fetchStart();

    await delay(1000 * warmup);
    const cnts0 = warmup === 0 ? undefined : await this.fetchProgressCnts();
    await delay(1000 * duration);
    const cnts1 = await this.fetchProgressCnts();
    await Promise.all(this.eachTrafficDir((cLabel) => this[`c${cLabel}`].stopFetch(this.state.tasks[cLabel])));

    let totalPackets = 0;
    let totalSeconds = 0;
    for (const [i, cnt1d] of cnts1.entries()) {
      for (const [j, cnt1] of cnt1d.entries()) {
        const cnt0: Pick<FetchCounters, "elapsed" | "finished" | "nRxData"> = cnts0?.[i]?.[j] ?? { elapsed: 0, nRxData: 0 };
        totalPackets += Number(cnt1.nRxData) - Number(cnt0.nRxData);
        totalSeconds += (Number(cnt1.finished ?? cnt1.elapsed) - Number(cnt0.finished ?? cnt0.elapsed)) / 1e9;
      }
    }
    const avgSeconds = totalSeconds / cnts1.length / cnts1[0]!.length;
    const pps = totalPackets / avgSeconds;
    return {
      duration: avgSeconds,
      pps,
      bps: pps * payloadLen * 8,
    };
  }

  private eachTrafficDir<R>(f: (cLabel: TgNodeLabel, pLabel: TgNodeLabel) => R): R[] {
    return trafficDirProducers[this.opts.trafficDir].map((pLabel: TgNodeLabel) => {
      const cLabel = trafficDirProducerToConsumer[pLabel];
      return f(cLabel, pLabel);
    });
  }

  private async fetchStart(): Promise<void> {
    const fileVersionTime = BigInt.asUintN(24, BigInt(Math.trunc(Date.now() / 1000))) << 8n;
    const {
      nFwds,
      producerKind,
      nFlows,
      interestNameLen,
      dataMatch,
      segmentEnd,
    } = this.opts;
    const comp2 = uniqueRandomVector(nFlows, 1024);
    await Promise.all(this.eachTrafficDir(async (cLabel, pLabel) => {
      const tasks: FetchTaskDef[] = [];
      for (let j = 0; j < nFlows; ++j) {
        const prefix3 = `/${pLabel}/${j % nFwds}/${comp2[j]}`;
        switch (producerKind) {
          case "pingserver":
            tasks.push({
              prefix: `${prefix3}${"/I".repeat(interestNameLen - 4)}`,
              canBePrefix: dataMatch === "prefix",
              mustBeFresh: true,
              segmentEnd,
            });
            break;
          case "fileserver": {
            const fileVersion = (this.state.fileServerVersionBypassHi[pLabel] << 32n) | fileVersionTime | BigInt(j);
            assert(fileVersion > 0xFFFFFFFFn);
            const fileVersionCompV = hexPad(fileVersion, 16).replace(/[\dA-F]{2}/g, "%$&");
            tasks.push({
              prefix: `${prefix3}/54=${fileVersionCompV}`,
              segmentEnd,
            });
            break;
          }
        }
      }
      this.state.tasks[cLabel] = await this[`c${cLabel}`].startFetch(this.state.fetcher[cLabel], tasks);
    }));
  }

  private fetchProgressCnts(): Promise<FetchCounters[][]> {
    return Promise.all(this.eachTrafficDir((cLabel) => this[`c${cLabel}`].getFetchProgress(this.state.tasks[cLabel])));
  }
}

interface State {
  /** TgNodeLabel => forwarder side face ID */
  face: Record<string, string>;
  /** whether NDT duplicates are detected */
  ndtDuplicate: boolean;
  /** TgNodeLabel => fetcher ID */
  fetcher: Record<string, string>;
  /** TgNodeLabel => fileserver versionBypassHi */
  fileServerVersionBypassHi: Record<string, bigint>;
  /** TgNodeLabel => fetcher task IDs */
  tasks: Record<string, string[]>;
}

function makeInitialState(): State {
  return {
    face: {},
    ndtDuplicate: false,
    fetcher: {},
    fileServerVersionBypassHi: {},
    tasks: {},
  };
}

type TgNodeLabel = "A" | "B";

const tgNodeLabels: readonly TgNodeLabel[] = ["A", "B"];

const trafficDirProducers: Record<BenchmarkOptions.TrafficDir, readonly TgNodeLabel[]> = {
  1: ["A"],
  2: ["A", "B"],
};

const trafficDirProducerToConsumer: Record<TgNodeLabel, TgNodeLabel> = {
  A: "B",
  B: "A",
};

const vxlanLocatorFields: Omit<VxlanLocator, Exclude<keyof EtherLocator, "scheme">> = {
  scheme: "vxlan",
  localIP: "192.168.118.0",
  remoteIP: "192.168.118.0",
  vxlan: 0,
  innerLocal: "02:00:00:ff:ff:ff",
  innerRemote: "02:00:00:ff:ff:ff",
};
