#include <asm/uaccess.h>
#include <linux/init.h>
#include <linux/module.h>
#include <linux/proc_fs.h>
#include <linux/random.h>
#include <linux/sched.h>

MODULE_AUTHOR("Austin Pohlmann");
MODULE_LICENSE("GPL v2");

#ifdef __i386__
	#ifndef __KERNEL__
		#error "This kernel configuration isn't supported :("
	#else
		static const int regs_len = 12;
		static const char reg_names[12][8] = {"bx","cx","dx","si","di","bp","ax","ds","es","fs","gs","orig_ax"};
	#endif
#else
	#ifndef __KERNEL__
		static const int regs_len = 16;
		static const char reg_names[16][9] = {"r15","r14","r13","r12","rbp","rbx","r11","r10","r9","r8","rax","rcx","rdx","rsi","rdi","orig_rax"};
	#else
		static const int regs_len = 16;
		static const char reg_names[16][8] = {"r15","r14","r13","r12","bp","bx","r11","r10","r9","r8","ax","cx","dx","si","di","orig_ax"};
	#endif
#endif

static int p_id,freg,fbit;

static struct proc_dir_entry *pwe;
int kfi_write(struct file *f, const char __user *buff, unsigned long count, void *data){
	char *id, *start, *reg_string = NULL, *bit_string = NULL;
	struct pid *pid_s;
	struct task_struct *task;
	struct pt_regs *regs;
	unsigned long *registers;
	u8 nums[2];
	int ret, isStopped;
	wait_queue_head_t wait;
	ret = -1;
	id = kzalloc(500*sizeof(char), GFP_KERNEL);
	// Copy from the user space pointer
	if(unlikely(copy_from_user(id,buff,500))){
		pr_err("kfi: copy_from_user returned non-zero");
		ret = -EFAULT;
		goto freeid;
	}
	// Parse the string
	// "pid" or "pid reg" or "pid reg bit"
	for(start = id; *start; start++){
		if (*start == ' '){
			while (*start == ' ') {
				*(start++) = 0;
			}
			if (reg_string == NULL) {
				reg_string = start;
			} else {
				bit_string = start;
				break;
			}
		}
	}
	p_id = simple_strtoul(id,NULL,0);
	if (reg_string != NULL) {
		freg = simple_strtoul(reg_string,NULL,0);
	}
	if (bit_string != NULL) {
		fbit = simple_strtoul(bit_string,NULL,0);
	}
	pid_s = find_get_pid(p_id);
	if(unlikely(!pid_s)) {
		pr_err("kfi: pid_s null");
		goto freeid;
	}
	task = pid_task(pid_s,PIDTYPE_PID);
	if(unlikely(!task)){
		pr_err("kfi: task null");
		goto freeid;
	}
	if(unlikely(kill_pid(pid_s,SIGSTOP,1))){
		pr_err("kfi: Error stopping process");
		goto freeid;
	}
	init_waitqueue_head(&wait);
	isStopped = wait_event_timeout(wait, task->state & __TASK_STOPPED, usecs_to_jiffies(500));
	if(!isStopped){
		pr_err("kfi: waited 500 usecs, task still not stopped");
		kill_pid(pid_s,SIGCONT,1);
		goto freeid;
	}
	regs = task_pt_regs(task);
	registers = (unsigned long *)regs;
	get_random_bytes(nums, 2);
	// If no reg/bit was specified, or if not a valid input, pick random values
	if (reg_string == NULL || freg >= regs_len || freg < 0){
		freg = nums[0] % regs_len;
	}
	if (bit_string == NULL || fbit >= sizeof(unsigned long) * 8 || fbit < 0){
		fbit = nums[1] % (sizeof(unsigned long) * 8);
	}
	pr_info("kfi: pid %d reg %s bit %d",p_id,reg_names[freg],fbit);
	registers[freg] ^= 1 << fbit;
	if(unlikely(kill_pid(pid_s,SIGCONT,1))){
		pr_err("kfi: Error continuing process");
		goto freeid;
	}
	ret = count;
	freeid:
	kfree(id);
	return ret;
}
int kfi_read(char *buf,char **start,off_t offset,int count,int *eof,void *data){
	*eof = 1;
	return sprintf(buf,"Injected process %d, register %s, bit %d",p_id,reg_names[freg],fbit);	
}
static int initialize(void){
	pr_info("KFI init");
	pwe = create_proc_entry("kfi",0666,NULL);
	if(!pwe){
		pr_err("Error creating pfi proc entry");
		return -1;
	}
	pwe->read_proc = kfi_read;
	pwe->write_proc = kfi_write;
	p_id = freg = fbit = 0;
	return 0;
}

static void escape(void){
	pr_info("KFI exit");
	remove_proc_entry("kfi",NULL);
}

module_init(initialize);
module_exit(escape);
