import { useState } from "react";
import { Tasks, type Task } from "../mygo";
import { useTasks } from "../store/tasks";
import { Button } from "./arc/button/button";
import { Checkbox } from "./arc/checkbox/checkbox";
import { Dialog, DialogContent } from "./arc/dialog/dialog";

/**
 * RemoveDialog：删除任务确认框。询问是否连同磁盘上的文件一并删除；
 * 取消则只删记录保留文件。确认后从本地列表移除该任务。
 */
export function RemoveDialog({ task, onClose }: { task: Task; onClose: () => void }) {
  const removeLocal = useTasks((s) => s.removeLocal);
  const [withFiles, setWithFiles] = useState(false);

  function remove() {
    void Tasks.remove(task.id, withFiles)
      .then(() => removeLocal(task.id))
      .finally(onClose);
  }

  return (
    <Dialog
      open
      onOpenChange={(next) => {
        if (!next) onClose();
      }}
    >
      <DialogContent title="删除任务">
        <div className="flex flex-col gap-4">
          <p className="truncate text-sm" title={task.name}>
            确定删除「{task.name}」吗？
          </p>
          <div className="flex flex-col gap-1">
            <Checkbox
              label="同时删除磁盘中的文件（不可恢复）"
              checked={withFiles}
              onCheckedChange={(v) => setWithFiles(v === true)}
            />
            {withFiles && (
              <span className="break-all pl-6 text-xs text-muted" title={task.dir}>
                {task.dir}
              </span>
            )}
          </div>
          <div className="flex items-center justify-end gap-2">
            <Button variant="secondary" onClick={onClose}>
              取消
            </Button>
            <Button variant="danger" onClick={remove}>
              删除
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
