import { useState, useCallback } from "react";
import { Modal, Select, message, Typography } from "antd";
import { useCreateComparison, useExperiments } from "../../api/hooks";
import { useNavigate } from "react-router-dom";

const { Text } = Typography;

interface Props {
  experimentId: string;
  projectId: string;
  visible: boolean;
  onClose: () => void;
}

export function CompareCreateModal({ experimentId, projectId, visible, onClose }: Props) {
  const navigate = useNavigate();
  const [compareType, setCompareType] = useState<string>("general");
  const [candidateId, setCandidateId] = useState<string>("");
  const createComparison = useCreateComparison(projectId);
  const { data: allExperiments } = useExperiments(projectId);
  const otherExperiments = allExperiments?.filter((e) => e.id !== experimentId) ?? [];

  const handleSubmit = useCallback(async () => {
    if (!candidateId) {
      message.error("请选择 Candidate 实验");
      return;
    }
    try {
      const result = await createComparison.mutateAsync({
        baseline_experiment_id: experimentId,
        candidate_experiment_id: candidateId,
        comparison_type: compareType,
      });
      message.success("对比创建成功");
      onClose();
      navigate(`/comparisons/${result.id}`);
    } catch (err: any) {
      message.error(err?.message || "创建对比失败");
    }
  }, [candidateId, compareType, experimentId, projectId, createComparison, navigate, onClose]);

  return (
    <Modal
      title="创建实验对比"
      open={visible}
      onCancel={onClose}
      onOk={handleSubmit}
      confirmLoading={createComparison.isPending}
      okText="创建对比"
      cancelText="取消"
    >
      <div style={{ marginBottom: 16 }}>
        <div style={{ marginBottom: 8 }}>
          <Text strong>Baseline 实验</Text>
        </div>
        <Text code>{experimentId}</Text>
      </div>
      <div style={{ marginBottom: 16 }}>
        <div style={{ marginBottom: 8 }}>
          <Text strong>对比类型</Text>
        </div>
        <Select
          style={{ width: "100%" }}
          value={compareType}
          onChange={setCompareType}
          options={[
            { value: "general", label: "通用对比" },
            { value: "model_swap", label: "模型替换（对比不同模型时选择）" },
            { value: "ablation", label: "消融实验（对比 Harness 变更时选择）" },
          ]}
        />
      </div>
      <div style={{ marginBottom: 16 }}>
        <div style={{ marginBottom: 8 }}>
          <Text strong>选择 Candidate 实验</Text>
        </div>
        <Select
          style={{ width: "100%" }}
          placeholder="选择 Candidate 实验..."
          value={candidateId}
          onChange={setCandidateId}
          options={otherExperiments.map((e) => ({
            value: e.id,
            label: `${e.target_name} v${e.target_version || "?"}（${e.status === "completed" ? "已完成" : e.status}）`,
          }))}
          notFoundContent="当前项目没有其他实验可对比"
        />
      </div>
      <div>
        <Text type="secondary">
          回归检测将根据所选对比类型自动应用约束。对比创建后将自动跳转到对比报告页。
        </Text>
      </div>
    </Modal>
  );
}
