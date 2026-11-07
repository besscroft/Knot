import { create } from "zustand";
import { App, events, Tasks, type Task } from "../mygo";
import { setMediaBase } from "../lib/format";

interface TasksState {
  /** id → 任务快照 */
  byId: Record<string, Task>;
  /** 展示顺序 = 创建顺序（Go 侧 List 的顺序，事件到达时原位更新） */
  order: string[];
  loaded: boolean;
  /** 应用启动时拉取全量并订阅事件流；返回退订函数 */
  init: () => Promise<() => void>;
  upsert: (task: Task) => void;
  removeLocal: (id: string) => void;
}

export const useTasks = create<TasksState>((set, get) => ({
  byId: {},
  order: [],
  loaded: false,

  async init() {
    // 媒体地址先就位：任务列表一到，卡片封面/播放链接立即可用。
    try {
      setMediaBase(await App.mediaBase());
    } catch {
      // 未就绪（不应发生）：媒体请求会失败，不影响任务列表。
    }
    const tasks = await Tasks.list();
    set(() => {
      const byId: Record<string, Task> = {};
      for (const t of tasks) byId[t.id] = t;
      return { byId, order: tasks.map((t) => t.id), loaded: true };
    });
    return events.taskUpdated.on((task) => get().upsert(task));
  },

  upsert(task) {
    set((s) => {
      if (!(task.id in s.byId)) {
        return {
          byId: { ...s.byId, [task.id]: task },
          order: [...s.order, task.id],
        };
      }
      return { byId: { ...s.byId, [task.id]: task } };
    });
  },

  removeLocal(id) {
    set((s) => {
      const byId = { ...s.byId };
      delete byId[id];
      return { byId, order: s.order.filter((x) => x !== id) };
    });
  },
}));
